package appeal

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	"receipts/internal/events"
	"receipts/internal/receipt"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

type reviewClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *reviewClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *reviewClock) Add(d time.Duration) { c.mu.Lock(); c.at = c.at.Add(d); c.mu.Unlock() }

type reviewFixture struct {
	db      *sqlite.DB
	ledger  *store.Store
	runtime *channelruntime.Manager
	clock   *reviewClock
	a       auth.ChannelContext
	ch      *channelruntime.Channel
}

func newReviewFixture(t *testing.T) *reviewFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	clock := &reviewClock{at: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "review.db"), Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ledger, err := store.New(ctx, db, store.Retention{}, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(ctx, "review-platform", "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	live, err := ledger.StartSession(ctx, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('review-owner','twitch','review-platform','reviewer',?,X'01',0,'[]','connected',0)`, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := auth.ChannelContext{AccountID: "review-owner", ChannelID: channel.ID, LiveSessionID: live.ID, Generation: 1}
	runtime := channelruntime.New(channelruntime.Config{MaxChannels: 2})
	ch, err := runtime.StartVerified(ctx, a, ledger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Stop(a) })
	clock.Add(10 * time.Second)
	return &reviewFixture{db, ledger, runtime, clock, a, ch}
}
func (f *reviewFixture) pending(t *testing.T, name string) (receipt.Receipt, string, receipt.Segment) {
	t.Helper()
	ctx := context.Background()
	original := receipt.Segment{ID: "original-" + name, SessionID: f.a.LiveSessionID, Start: 0, End: time.Second, Text: "I will never take that shortcut again.", Attribution: "broadcaster"}
	if err := f.ledger.AddSegment(ctx, original); err != nil {
		t.Fatal(err)
	}
	ev := receipt.EvidenceInput{SourceID: original.ID, Text: original.Text, Start: original.Start, End: original.End, Attribution: original.Attribution}
	r, err := f.ledger.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: f.a.ChannelID, SessionID: f.a.LiveSessionID, Claim: "Never take that shortcut.", ClippedBy: "river", Evidence: []receipt.EvidenceInput{ev}})
	if err != nil {
		t.Fatal(err)
	}
	later := receipt.Segment{ID: "later-" + name, SessionID: f.a.LiveSessionID, Start: 4 * time.Second, End: 5 * time.Second, Text: "I am taking that shortcut again.", Attribution: "broadcaster"}
	if err := f.ledger.AddSegment(ctx, later); err != nil {
		t.Fatal(err)
	}
	_, err = f.ledger.RecordDetection(ctx, receipt.DetectionInput{ChannelID: f.a.ChannelID, ReceiptID: r.ID, Evidence: receipt.EvidenceInput{SourceID: later.ID, Text: later.Text, Start: later.Start, End: later.End, Attribution: later.Attribution}, Outcome: receipt.NeedsReview})
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := f.ledger.PendingReviews(ctx, f.a.AccountID, f.a.ChannelID, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range reviews {
		var id string
		if err := f.db.Reader().QueryRow(`SELECT d.receipt_id FROM reviews v JOIN detections d ON d.id=v.detection_id WHERE v.id=?`, v.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if id == r.ID {
			return r, v.ID, later
		}
	}
	t.Fatal("review not found")
	return r, "", later
}
func (f *reviewFixture) service() ReviewService {
	return ReviewService{Store: f.ledger, Runtime: f.runtime, Cooldown: 30 * time.Second, Now: f.clock.Now}
}
func TestReviewApprovalIsIdempotentAndPublishesOnce(t *testing.T) {
	f := newReviewFixture(t)
	r, id, _ := f.pending(t, "first")
	ctx := context.Background()
	sub := f.ch.Events.Broker.Subscribe(ctx, events.TopicOverlay)
	defer sub.Cancel()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- f.service().Resolve(ctx, f.a, id, "same-action", true) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.ch.Events.Sequence() != 1 {
		t.Fatalf("stamp count=%d", f.ch.Events.Sequence())
	}
	frame := (<-sub.Events).Data.(events.OverlayEvent)
	if frame.Receipt == nil || frame.Receipt.ID != r.ID || frame.Receipt.ClippedBy != "river" || !frame.OccurredAt.Equal(f.clock.Now()) {
		t.Fatalf("public stamp=%+v", frame)
	}
	if err := f.runtime.SetPaused(f.a, true); err != nil {
		t.Fatal(err)
	}
	if err := f.service().Resolve(ctx, f.a, id, "same-action", true); err != nil {
		t.Fatalf("retry after pause=%v", err)
	}
	if err := f.service().Resolve(ctx, f.a, id, "same-action", false); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("conflicting key=%v", err)
	}
	if f.ch.Events.Sequence() != 1 {
		t.Fatal("retry emitted stamp")
	}
}
func TestReviewRejectionIsPrivateAndVersionChangesFailClosed(t *testing.T) {
	f := newReviewFixture(t)
	r, id, _ := f.pending(t, "reject")
	ctx := context.Background()
	if err := f.runtime.SetPaused(f.a, true); err != nil {
		t.Fatal(err)
	}
	if err := f.service().Resolve(ctx, f.a, id, "reject", false); err != nil {
		t.Fatal(err)
	}
	if f.ch.Events.Sequence() != 0 {
		t.Fatal("private rejection emitted event")
	}
	_, id2, _ := f.pending(t, "version")
	candidate, err := f.ledger.ReviewCandidate(ctx, f.a.AccountID, f.a.ChannelID, id2)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ledger.SetClip(ctx, f.a.ChannelID, candidate.ID, receipt.ClipUnavailable, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ledger.ResolveReviewAction(ctx, authority(f.a), candidate, id2, "stale", true, false, 30*time.Second); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("stale version=%v", err)
	}
	_ = r
}
func TestReviewGuardRejectsInvalidCurrentState(t *testing.T) {
	cases := map[string]func(*testing.T, *reviewFixture, receipt.Receipt, receipt.Segment){
		"void": func(t *testing.T, f *reviewFixture, r receipt.Receipt, _ receipt.Segment) {
			if err := f.ledger.Void(context.Background(), f.a.ChannelID, r.ID); err != nil {
				t.Fatal(err)
			}
		},
		"sensitive": func(t *testing.T, f *reviewFixture, r receipt.Receipt, _ receipt.Segment) {
			_, err := f.db.Writer().Exec(`UPDATE receipts SET sensitive=1 WHERE id=?`, r.ID)
			if err != nil {
				t.Fatal(err)
			}
		},
		"expiry": func(t *testing.T, f *reviewFixture, r receipt.Receipt, _ receipt.Segment) {
			_, err := f.db.Writer().Exec(`UPDATE receipts SET expires_at=? WHERE id=?`, f.clock.Now().UnixMilli(), r.ID)
			if err != nil {
				t.Fatal(err)
			}
		},
		"source": func(t *testing.T, f *reviewFixture, _ receipt.Receipt, s receipt.Segment) {
			_, err := f.db.Writer().Exec(`UPDATE transcript_segments SET text='changed' WHERE id=?`, s.ID)
			if err != nil {
				t.Fatal(err)
			}
		},
		"session": func(t *testing.T, f *reviewFixture, _ receipt.Receipt, _ receipt.Segment) {
			if err := f.ledger.EndSession(context.Background(), f.a.ChannelID, f.a.LiveSessionID); err != nil {
				t.Fatal(err)
			}
		},
		"pause": func(t *testing.T, f *reviewFixture, _ receipt.Receipt, _ receipt.Segment) {
			if err := f.runtime.SetPaused(f.a, true); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newReviewFixture(t)
			r, id, source := f.pending(t, name)
			mutate(t, f, r, source)
			if err := f.service().Resolve(context.Background(), f.a, id, "approve", true); err == nil {
				t.Fatal("invalid review approved")
			}
			if f.ch.Events.Sequence() != 0 {
				t.Fatal("invalid stamp emitted")
			}
		})
	}
}
func TestApprovedReviewCooldownAndVoidDoNotResurrectOldStamp(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	a, ra, _ := f.pending(t, "a")
	b, rb, _ := f.pending(t, "b")
	if err := f.service().Resolve(ctx, f.a, ra, "a", true); err != nil {
		t.Fatal(err)
	}
	if err := f.service().Resolve(ctx, f.a, rb, "too-soon", true); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("cooldown=%v", err)
	}
	f.clock.Add(30 * time.Second)
	if err := f.service().Resolve(ctx, f.a, rb, "b", true); err != nil {
		t.Fatal(err)
	}
	stamp, err := f.ledger.CurrentStamp(ctx, f.a.ChannelID, f.a.LiveSessionID)
	if err != nil || stamp == nil || stamp.ID != b.ID {
		t.Fatalf("approval order stamp=%+v err=%v", stamp, err)
	}
	if err := f.ledger.Void(ctx, f.a.ChannelID, b.ID); err != nil {
		t.Fatal(err)
	}
	stamp, err = f.ledger.CurrentStamp(ctx, f.a.ChannelID, f.a.LiveSessionID)
	if err != nil || stamp != nil {
		t.Fatalf("old stamp %s resurrected: %+v %v", a.ID, stamp, err)
	}
}

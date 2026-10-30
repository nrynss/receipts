package detect

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	"receipts/internal/receipt"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

type judgeFunc func(context.Context, receipt.Receipt, receipt.Segment) (Verdict, error)

func (f judgeFunc) Judge(c context.Context, r receipt.Receipt, s receipt.Segment) (Verdict, error) {
	return f(c, r, s)
}

type safe struct{}

func (safe) Sensitive(_ context.Context, s string) (bool, error) { return s == "private", nil }
func TestRecordedDecisionsAndDurableGuards(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	c, err := ledger.CreateChannel(ctx, "streamer", "streamer")
	if err != nil {
		t.Fatal(err)
	}
	session, err := ledger.StartSession(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','streamer','streamer',?,X'00',?,'[]','connected',?)`, c.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	a := auth.ChannelContext{AccountID: "owner", ChannelID: c.ID, LiveSessionID: session.ID, Generation: 1}
	manager := channelruntime.New(channelruntime.Config{})
	ch, err := manager.StartVerified(ctx, a, ledger)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(a)
	add := func(id, text string) receipt.Segment {
		t.Helper()
		v := receipt.Segment{ID: id, SessionID: session.ID, Text: text, Attribution: "broadcaster", SpeakerID: "streamer"}
		if err := ledger.AddSegment(ctx, v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	original := add("original", "I will never take the shortcut again.")
	r, err := ledger.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: session.ID, Claim: "Never take the shortcut", ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{{SourceID: original.ID, Text: original.Text, Attribution: original.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../testdata/models/t22-judge.json")
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := LoadRecorded(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var result Verdict
	svc := Service{Store: ledger, Runtime: manager, Sensitive: safe{}, Judge: judgeFunc(func(context.Context, receipt.Receipt, receipt.Segment) (Verdict, error) { return result, nil })}
	miss := add("miss", "This shortcut looks tricky.")
	result = recorded["near_miss"]
	if err = svc.Check(ctx, a, r.ID, miss); err != nil {
		t.Fatal(err)
	}
	if ch.Events.Sequence() != 0 {
		t.Fatal("near miss emitted")
	}
	uncertain := add("uncertain", "Maybe I should try it.")
	result = recorded["uncertain"]
	if err = svc.Check(ctx, a, r.ID, uncertain); err != nil {
		t.Fatal(err)
	}
	reviews, err := ledger.PendingReviews(ctx, a.AccountID, a.ChannelID, 100)
	if err != nil || len(reviews) != 1 {
		t.Fatalf("reviews %v %v", reviews, err)
	}
	delayed := add("delayed", "I will take the shortcut.")
	svc.Judge = judgeFunc(func(context.Context, receipt.Receipt, receipt.Segment) (Verdict, error) {
		if err := manager.SetPaused(a, true); err != nil {
			t.Fatal(err)
		}
		return Contradicts, nil
	})
	if err = svc.Check(ctx, a, r.ID, delayed); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("paused delayed result: %v", err)
	}
	if err = manager.SetPaused(a, false); err != nil {
		t.Fatal(err)
	}
	svc.Judge = judgeFunc(func(context.Context, receipt.Receipt, receipt.Segment) (Verdict, error) { return result, nil })
	foreign := delayed
	foreign.SessionID = "foreign"
	if err = svc.Check(ctx, a, r.ID, foreign); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("foreign source: %v", err)
	}
	uncertainSource := delayed
	uncertainSource.Attribution = "uncertain"
	if err = svc.Check(ctx, a, r.ID, uncertainSource); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("uncertain source: %v", err)
	}
	sensitive := delayed
	sensitive.Text = "private"
	if err = svc.Check(ctx, a, r.ID, sensitive); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("sensitive source: %v", err)
	}
	stamp := add("stamp", "Fine, I'll take the shortcut.")
	result = recorded["contradiction"]
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- svc.Check(ctx, a, r.ID, stamp) }()
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		}
	}
	if success != 1 || ch.Events.Sequence() != 1 {
		t.Fatalf("success %d sequence %d", success, ch.Events.Sequence())
	}
	current, err := ledger.CurrentStamp(ctx, a.ChannelID, a.LiveSessionID)
	if err != nil || current == nil || current.ID != r.ID {
		t.Fatalf("stamp %v %v", current, err)
	}
	if err = ledger.Void(ctx, a.ChannelID, r.ID); err != nil {
		t.Fatal(err)
	}
	current, err = ledger.CurrentStamp(ctx, a.ChannelID, a.LiveSessionID)
	if err != nil || current != nil {
		t.Fatalf("void stamp %v %v", current, err)
	}
}
func TestDelayedPauseAndCancellation(t *testing.T) {
	// The runtime guard must serialize the final commit with pause and stop.
	ctx := context.Background()
	m := channelruntime.New(channelruntime.Config{})
	a := auth.ChannelContext{AccountID: "a", ChannelID: "c", LiveSessionID: "s", Generation: 1}
	ch, err := m.Start(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.SetPaused(a, true); err != nil {
		t.Fatal(err)
	}
	if err = m.WithActive(a, func(c *channelruntime.Channel) error {
		if c.Paused() {
			return store.ErrNotEligible
		}
		return nil
	}); !errors.Is(err, store.ErrNotEligible) {
		t.Fatal(err)
	}
	if err = m.Stop(a); err != nil {
		t.Fatal(err)
	}
	if err = m.WithActive(a, func(*channelruntime.Channel) error { return nil }); !errors.Is(err, channelruntime.ErrStale) {
		t.Fatal(err)
	}
	if ch.Context.Err() == nil {
		t.Fatal("not cancelled")
	}
}

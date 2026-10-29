package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"receipts/internal/receipt"
)

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func openTestStore(t *testing.T, path string, clock *testClock, ret Retention) *Store {
	t.Helper()
	s, err := Open(context.Background(), Config{Path: path, Retention: ret, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func setup(t *testing.T, ret Retention) (*Store, *testClock, receipt.Channel, receipt.Session) {
	t.Helper()
	clock := &testClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	s := openTestStore(t, filepath.Join(t.TempDir(), "ledger.db"), clock, ret)
	c, err := s.CreateChannel(context.Background(), "twitch-42", "streamer")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.StartSession(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, clock, c, session
}
func segment(t *testing.T, s *Store, sessionID, text string, start time.Duration) receipt.Segment {
	t.Helper()
	v := receipt.Segment{SessionID: sessionID, Start: start, End: start + 5*time.Second, Text: text, SpeakerID: "broadcaster-1", Attribution: "broadcaster"}
	if err := s.AddSegment(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	got, err := s.Transcript(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, seg := range got {
		if seg.Start == start {
			return seg
		}
	}
	t.Fatal("new segment missing")
	return receipt.Segment{}
}

func TestMigrationsRepeatAndReopenPreservesReceiptAndVerdict(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ledger.db")
	clock := &testClock{t: time.Now().UTC()}
	s := openTestStore(t, path, clock, Retention{})
	channel, err := s.CreateChannel(ctx, "platform", "login")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateChannel(ctx, "platform", "other-login"); err != nil {
		t.Fatal(err)
	}
	session, err := s.StartSession(ctx, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	seg := segment(t, s, session.ID, "I will never pick Vex again", 0)
	r, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: channel.ID, SessionID: session.ID, Claim: "Never pick Vex", ClippedBy: "viewer-1", Evidence: []receipt.EvidenceInput{{SourceID: seg.ID, Text: seg.Text, Start: seg.Start, End: seg.End, Attribution: seg.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Decide(ctx, channel.ID, r.ID, "admit-once", receipt.Admitted, "I said it")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Decide(ctx, channel.ID, r.ID, "admit-once", receipt.Admitted, "I said it")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != again.ID {
		t.Fatal("repeated decision created another verdict")
	}
	newer, err := s.Decide(ctx, channel.ID, r.ID, "context-once", receipt.ContextChanged, "context changed")
	if err != nil {
		t.Fatal(err)
	}
	versionBefore, err := s.GetReceipt(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	retriedOld, err := s.Decide(ctx, channel.ID, r.ID, "admit-once", receipt.Admitted, "I said it")
	if err != nil || retriedOld.ID != first.ID {
		t.Fatalf("old key retry: %#v %v", retriedOld, err)
	}
	versionAfter, err := s.GetReceipt(ctx, r.ID)
	if err != nil || versionAfter.Version != versionBefore.Version || versionAfter.Verdict != newer.Outcome {
		t.Fatalf("old retry overwrote latest verdict: %#v %#v %v", versionBefore, versionAfter, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s2 := openTestStore(t, path, clock, Retention{})
	rr, err := s2.GetReceipt(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rr.Verdict != receipt.ContextChanged || rr.Validity != receipt.Valid {
		t.Fatalf("reopened receipt = %#v", rr)
	}
}

func TestRetentionPreservesEvidenceAndVoidExcludesPublicData(t *testing.T) {
	s, _, c, session := setup(t, Retention{MaxSegments: 2})
	ctx := context.Background()
	first := segment(t, s, session.ID, "claim", 0)
	r, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: session.ID, Claim: "the claim", ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{{SourceID: first.ID, Text: first.Text, Start: first.Start, End: first.End, Attribution: first.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	segment(t, s, session.ID, "later 1", 10*time.Second)
	segment(t, s, session.ID, "later 2", 20*time.Second)
	segments, err := s.Transcript(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 || segments[0].Text != "later 1" {
		t.Fatalf("transcript retention: %#v", segments)
	}
	ev, err := s.Evidence(ctx, r.ID)
	if err != nil || len(ev) != 1 || ev[0].Text != "claim" || ev[0].SourceID != first.ID {
		t.Fatalf("durable evidence: %#v, %v", ev, err)
	}
	if err = s.Void(ctx, c.ID, r.ID); err != nil {
		t.Fatal(err)
	}
	public, err := s.PublicReceipts(ctx, c.ID, 0)
	if err != nil || len(public) != 0 {
		t.Fatalf("void receipt leaked to public retrieval: %#v %v", public, err)
	}
	ev, err = s.Evidence(ctx, r.ID)
	if err != nil || len(ev) != 0 {
		t.Fatalf("void evidence leaked: %#v %v", ev, err)
	}
	if _, err = s.RecordDetection(ctx, receipt.DetectionInput{ChannelID: c.ID, ReceiptID: r.ID, Evidence: receipt.EvidenceInput{SourceID: first.ID, Text: "contradiction", Start: 0, End: 5 * time.Second}, Outcome: receipt.Stamp}); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("detection on void receipt err=%v", err)
	}
}

func TestSourceAndSessionOwnershipAndInvalidTransitions(t *testing.T) {
	s, _, c, session := setup(t, Retention{})
	ctx := context.Background()
	seg := segment(t, s, session.ID, "claim", 0)
	other, err := s.CreateChannel(ctx, "another", "other")
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := s.StartSession(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: session.ID, Claim: "wrong source", ClippedBy: "v", Evidence: []receipt.EvidenceInput{{SourceID: seg.ID, Text: "claim", Start: 0, End: 20 * time.Second}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("out-of-range evidence err=%v", err)
	}
	r, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: session.ID, Claim: "claim", ClippedBy: "v", Evidence: []receipt.EvidenceInput{{SourceID: seg.ID, Text: seg.Text, Start: seg.Start, End: seg.End, Attribution: seg.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordDetection(ctx, receipt.DetectionInput{ChannelID: other.ID, ReceiptID: r.ID, Evidence: receipt.EvidenceInput{SourceID: seg.ID, Text: "line", Start: 0, End: time.Second}, Outcome: receipt.Stamp}); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("cross-channel detection err=%v", err)
	}
	otherSeg := segment(t, s, otherSession.ID, "same text", time.Second)
	if _, err = s.RecordDetection(ctx, receipt.DetectionInput{ChannelID: c.ID, ReceiptID: r.ID, Evidence: receipt.EvidenceInput{SourceID: otherSeg.ID, Text: otherSeg.Text, Start: otherSeg.Start, End: otherSeg.End, Attribution: otherSeg.Attribution}, Outcome: receipt.Stamp}); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("foreign source session detection err=%v", err)
	}
	if err = s.EndSession(ctx, c.ID, session.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.EndSession(ctx, c.ID, session.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("repeated end err=%v", err)
	}
	if _, err = s.RecordDetection(ctx, receipt.DetectionInput{ChannelID: c.ID, ReceiptID: r.ID, Evidence: receipt.EvidenceInput{SourceID: seg.ID, Text: seg.Text, Start: seg.Start, End: seg.End, Attribution: seg.Attribution}, Outcome: receipt.Stamp}); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("ended source session detection err=%v", err)
	}
	if err = s.AddSegment(ctx, receipt.Segment{SessionID: session.ID, Start: time.Second, End: 2 * time.Second, Text: "late"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("segment after end err=%v", err)
	}
	if otherSession.ID == session.ID {
		t.Fatal("session IDs collided")
	}
}

func TestDuplicateDetectionDecisionAndConcurrentUniqueVotes(t *testing.T) {
	s, clock, c, session := setup(t, Retention{})
	ctx := context.Background()
	claimSeg := segment(t, s, session.ID, "I will never pick Vex again", 0)
	seg := segment(t, s, session.ID, "I picked Vex", 10*time.Second)
	r, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: session.ID, Claim: "Never pick Vex", ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{{SourceID: claimSeg.ID, Text: claimSeg.Text, Start: claimSeg.Start, End: claimSeg.End, Attribution: claimSeg.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	input := receipt.DetectionInput{ChannelID: c.ID, ReceiptID: r.ID, Evidence: receipt.EvidenceInput{SourceID: seg.ID, Text: seg.Text, Start: seg.Start, End: seg.End, Attribution: seg.Attribution}, Outcome: receipt.NeedsReview}
	d1, err := s.RecordDetection(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := s.RecordDetection(ctx, input)
	if err != nil || d1.ID != d2.ID {
		t.Fatalf("duplicate detection: %#v %#v %v", d1, d2, err)
	}
	rev1, err := s.DecideReview(ctx, c.ID, d1.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	rev2, err := s.DecideReview(ctx, c.ID, d1.ID, true)
	if err != nil || rev1.ID != rev2.ID {
		t.Fatalf("review retry: %#v %#v %v", rev1, rev2, err)
	}
	a, err := s.OpenAppeal(ctx, c.ID, r.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := s.CastVote(ctx, c.ID, a.ID, "voter-same", receipt.VoteUphold)
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Errorf("concurrent duplicate vote: %v", e)
		}
	}
	for i := 0; i < 5; i++ {
		if _, err = s.CastVote(ctx, c.ID, a.ID, fmt.Sprintf("voter-%d", i), receipt.VoteUphold); err != nil {
			t.Fatal(err)
		}
	}
	clock.Add(time.Minute)
	v, err := s.FinalizeAppeal(ctx, c.ID, a.ID, "appeal-close")
	if err != nil || v.Outcome != receipt.Upheld {
		t.Fatalf("appeal decision: %#v %v", v, err)
	}
	r2, err := s.GetReceipt(ctx, r.ID)
	if err != nil || r2.Verdict != receipt.Upheld {
		t.Fatalf("receipt verdict: %#v %v", r2, err)
	}
	a2, err := s.OpenAppeal(ctx, c.ID, r.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Minute)
	inconclusive, err := s.FinalizeAppeal(ctx, c.ID, a2.ID, "appeal-no-votes")
	if err != nil || inconclusive.Outcome != receipt.Inconclusive {
		t.Fatalf("no-vote appeal: %#v %v", inconclusive, err)
	}
	again, err := s.FinalizeAppeal(ctx, c.ID, a2.ID, "appeal-no-votes")
	if err != nil || again.ID != inconclusive.ID {
		t.Fatalf("appeal retry: %#v %v", again, err)
	}
	if _, err = s.FinalizeAppeal(ctx, c.ID, a.ID, "appeal-no-votes"); !errors.Is(err, ErrConflict) {
		t.Fatalf("appeal decision key reused across appeals: %v", err)
	}
}

func TestReviewCannotApproveAfterVoidAndExpiryPrunesAcrossSessions(t *testing.T) {
	s, clock, c, session := setup(t, Retention{ExpireAfter: time.Second, MaxSegments: 5})
	ctx := context.Background()
	claim := segment(t, s, session.ID, "I will never pick Vex", 0)
	segment(t, s, session.ID, "I picked Vex", 10*time.Second)
	r, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: session.ID, Claim: "Never pick Vex", ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{{SourceID: claim.ID, Text: claim.Text, Start: claim.Start, End: claim.End, Attribution: claim.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EndSession(ctx, c.ID, session.ID); err != nil {
		t.Fatal(err)
	}
	next, err := s.StartSession(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	later := segment(t, s, next.ID, "I picked Vex", 0)
	d, err := s.RecordDetection(ctx, receipt.DetectionInput{ChannelID: c.ID, ReceiptID: r.ID, Evidence: receipt.EvidenceInput{SourceID: later.ID, Text: later.Text, Start: later.Start, End: later.End, Attribution: later.Attribution}, Outcome: receipt.NeedsReview})
	if err != nil {
		t.Fatalf("later-session contradiction rejected: %v", err)
	}
	if err = s.Void(ctx, c.ID, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DecideReview(ctx, c.ID, d.ID, true); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("approved voided review: %v", err)
	}
	clock.Add(2 * time.Second)
	newest, err := s.StartSession(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	segment(t, s, newest.ID, "new session", 0)
	old, err := s.Transcript(ctx, session.ID)
	if err != nil || len(old) != 0 {
		t.Fatalf("old-session transcript survived expiry: %#v %v", old, err)
	}
	var count int
	if err = s.db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM transcript_segments`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("global transcript prune count=%d err=%v", count, err)
	}
}

func TestExpiredAndSensitiveRecordsAreNotPublicOrStampable(t *testing.T) {
	s, clock, c, session := setup(t, Retention{})
	ctx := context.Background()
	seg := segment(t, s, session.ID, "private claim", 0)
	r, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: session.ID, Claim: "private", ClippedBy: "viewer", Sensitive: true, Evidence: []receipt.EvidenceInput{{SourceID: seg.ID, Text: seg.Text, Start: seg.Start, End: seg.End, Attribution: seg.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	public, err := s.PublicReceipts(ctx, c.ID, 10)
	if err != nil || len(public) != 0 {
		t.Fatalf("sensitive record leaked: %#v %v", public, err)
	}
	seg2 := segment(t, s, session.ID, "temporary claim", 10*time.Second)
	expiry := clock.Now().Add(time.Minute)
	transient, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: session.ID, Claim: "temporary", ClippedBy: "viewer", ExpiresAt: &expiry, Evidence: []receipt.EvidenceInput{{SourceID: seg2.ID, Text: seg2.Text, Start: seg2.Start, End: seg2.End, Attribution: seg2.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	clock.Add(2 * time.Hour)
	public, err = s.PublicReceipts(ctx, c.ID, 10)
	if err != nil || len(public) != 0 {
		t.Fatalf("sensitive or expired receipt leaked: %#v %v", public, err)
	}
	expired, err := s.GetReceipt(ctx, transient.ID)
	if err != nil || expired.Validity != receipt.Expired {
		t.Fatalf("expired validity = %#v %v", expired, err)
	}
	if _, err = s.RecordDetection(ctx, receipt.DetectionInput{ChannelID: c.ID, ReceiptID: r.ID, Evidence: receipt.EvidenceInput{SourceID: seg.ID, Text: seg.Text, Start: seg.Start, End: seg.End, Attribution: seg.Attribution}, Outcome: receipt.Stamp}); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("sensitive receipt stamp err=%v", err)
	}
	if _, err = s.RecordDetection(ctx, receipt.DetectionInput{ChannelID: c.ID, ReceiptID: transient.ID, Evidence: receipt.EvidenceInput{SourceID: seg2.ID, Text: seg2.Text, Start: seg2.Start, End: seg2.End, Attribution: seg2.Attribution}, Outcome: receipt.Stamp}); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("expired receipt stamp err=%v", err)
	}
}

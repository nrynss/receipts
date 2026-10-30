package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"receipts/internal/receipt"
)

func actionFixture(t *testing.T) (*Store, *testClock, DetectionAuthority, string) {
	t.Helper()
	s, clock, c, session := setup(t, Retention{})
	ctx := context.Background()
	_, err := s.db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','streamer',?,X'01',0,'','connected',0)`, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	seg := segment(t, s, session.ID, "I will return", 0)
	evidence := receipt.EvidenceInput{SourceID: seg.ID, Text: seg.Text, Start: seg.Start, End: seg.End, Attribution: seg.Attribution}
	r, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: session.ID, Claim: "Will return", ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{evidence}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RecordDetection(ctx, receipt.DetectionInput{ChannelID: c.ID, ReceiptID: r.ID, Evidence: evidence, Outcome: receipt.Stamp})
	if err != nil {
		t.Fatal(err)
	}
	return s, clock, DetectionAuthority{AccountID: "owner", ChannelID: c.ID, LiveSessionID: session.ID, Generation: 1}, r.ID
}
func TestReceiptActionsConcurrentIdempotency(t *testing.T) {
	s, _, a, id := actionFixture(t)
	ctx := context.Background()
	const n = 20
	var wg sync.WaitGroup
	results := make(chan ReceiptActionResult, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.ApplyReceiptAction(ctx, a, id, "one", "appeal", "", time.Minute)
			results <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	first := ""
	for r := range results {
		if first == "" {
			first = r.ID
		}
		if r.ID != first {
			t.Fatalf("distinct results: %s %s", first, r.ID)
		}
	}
	if _, e := s.ApplyReceiptAction(ctx, a, id, "one", "admit", "", time.Minute); !errors.Is(e, ErrConflict) {
		t.Fatalf("key reuse: %v", e)
	}
	if _, e := s.ApplyReceiptAction(ctx, a, id, "another", "appeal", "", time.Minute); !errors.Is(e, ErrConflict) {
		t.Fatalf("second appeal: %v", e)
	}
}
func TestReceiptActionAuthorityAndDecision(t *testing.T) {
	s, _, a, id := actionFixture(t)
	ctx := context.Background()
	bad := a
	bad.AccountID = "other"
	if _, e := s.ApplyReceiptAction(ctx, bad, id, "bad", "admit", "", time.Minute); !errors.Is(e, ErrNotEligible) {
		t.Fatalf("foreign owner: %v", e)
	}
	got, e := s.ApplyReceiptAction(ctx, a, id, "key", "context_changed", "new context", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	retry, e := s.ApplyReceiptAction(ctx, a, id, "key", "context_changed", "new context", time.Minute)
	if e != nil || retry.ID != got.ID || !retry.Replayed {
		t.Fatalf("retry: %#v %v", retry, e)
	}
	r, e := s.GetReceipt(ctx, id)
	if e != nil || r.Verdict != receipt.ContextChanged {
		t.Fatalf("verdict: %#v %v", r, e)
	}
	if e = s.Void(ctx, a.ChannelID, id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyReceiptAction(ctx, a, id, "next", "admit", "", time.Minute); !errors.Is(e, ErrNotEligible) {
		t.Fatalf("void: %v", e)
	}
}

func TestAppealVotesBoundariesAndRecovery(t *testing.T) {
	s, clock, a, id := actionFixture(t)
	ctx := context.Background()
	_, err := s.ApplyReceiptAction(ctx, a, id, "appeal", "appeal", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := s.VoteActive(ctx, a, "viewer-1", receipt.VoteUphold); err != nil || !changed {
		t.Fatalf("first vote %v %v", changed, err)
	}
	if changed, err := s.VoteActive(ctx, a, "viewer-1", receipt.VoteUphold); err != nil || changed {
		t.Fatalf("duplicate %v %v", changed, err)
	}
	if _, err := s.VoteActive(ctx, a, "viewer-1", receipt.VoteOverturn); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed vote %v", err)
	}
	if _, err := s.VoteActive(ctx, a, "viewer-2", receipt.VoteOverturn); err != nil {
		t.Fatal(err)
	}
	other := a
	other.ChannelID = "foreign"
	if _, err := s.VoteActive(ctx, other, "viewer-3", receipt.VoteUphold); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("foreign: %v", err)
	}
	clock.Add(time.Minute)
	if _, err := s.VoteActive(ctx, a, "viewer-3", receipt.VoteUphold); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("deadline vote: %v", err)
	}
	if changed, err := s.CloseDueAppeal(ctx, a); err != nil || !changed {
		t.Fatalf("close %v %v", changed, err)
	}
	if changed, err := s.CloseDueAppeal(ctx, a); err != nil || changed {
		t.Fatalf("repeat close %v %v", changed, err)
	}
	r, err := s.GetReceipt(ctx, id)
	if err != nil || r.Verdict != receipt.Inconclusive {
		t.Fatalf("tie verdict %v %v", r.Verdict, err)
	}
}

func TestExpiredAppealCancelsWithoutVerdict(t *testing.T) {
	s, clock, a, id := actionFixture(t)
	ctx := context.Background()
	_, err := s.db.Writer().ExecContext(ctx, `UPDATE receipts SET expires_at=? WHERE id=?`, millis(clock.Now().Add(time.Second)), id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ApplyReceiptAction(ctx, a, id, "appeal", "appeal", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Second)
	if changed, err := s.CloseDueAppeal(ctx, a); err != nil || !changed {
		t.Fatalf("expiry %v %v", changed, err)
	}
	r, err := s.GetReceipt(ctx, id)
	if err != nil || r.Verdict != receipt.Undecided {
		t.Fatalf("expiry verdict %v %v", r.Verdict, err)
	}
	var outcome string
	if err = s.db.Reader().QueryRowContext(ctx, `SELECT decision FROM appeals WHERE receipt_id=?`, id).Scan(&outcome); err != nil || outcome != "cancelled" {
		t.Fatalf("cancelled %q %v", outcome, err)
	}
}

func TestConcurrentFirstVotesAndNoVoteRecovery(t *testing.T) {
	s, clock, a, id := actionFixture(t)
	ctx := context.Background()
	if _, err := s.ApplyReceiptAction(ctx, a, id, "appeal", "appeal", "", time.Minute); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.VoteActive(ctx, a, "one-voter", receipt.VoteUphold); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	active, err := s.ActiveAppeal(ctx, a.ChannelID)
	if err != nil || active == nil || active.Public.UpholdVotes != 1 {
		t.Fatalf("concurrent votes %#v %v", active, err)
	}
	clock.Add(time.Minute)
	if _, err = s.CloseDueAppeal(ctx, a); err != nil {
		t.Fatal(err)
	}
	r, err := s.GetReceipt(ctx, id)
	if err != nil || r.Verdict != receipt.Upheld {
		t.Fatalf("result %v %v", r.Verdict, err)
	}
}

func TestNoVotesIsInconclusive(t *testing.T) {
	s, clock, a, id := actionFixture(t)
	ctx := context.Background()
	if _, err := s.ApplyReceiptAction(ctx, a, id, "appeal", "appeal", "", time.Second); err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Second)
	if _, err := s.CloseDueAppeal(ctx, a); err != nil {
		t.Fatal(err)
	}
	r, err := s.GetReceipt(ctx, id)
	if err != nil || r.Verdict != receipt.Inconclusive {
		t.Fatalf("result %v %v", r.Verdict, err)
	}
}

func TestStartupSweepClosesWithoutRuntimeAndOnlyOnce(t *testing.T) {
	s, clock, a, id := actionFixture(t)
	ctx := context.Background()
	if _, err := s.ApplyReceiptAction(ctx, a, id, "appeal", "appeal", "", time.Second); err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Second)
	// Disconnect the account: startup recovery must not invent a live session.
	if _, err := s.db.Writer().ExecContext(ctx, `UPDATE accounts SET status='reconnect_required' WHERE id=?`, a.AccountID); err != nil {
		t.Fatal(err)
	}
	n, err := s.RecoverDueAppeals(ctx)
	if err != nil || n != 1 {
		t.Fatalf("sweep %d %v", n, err)
	}
	n, err = s.RecoverDueAppeals(ctx)
	if err != nil || n != 0 {
		t.Fatalf("repeat %d %v", n, err)
	}
	r, err := s.GetReceipt(ctx, id)
	if err != nil || r.Verdict != receipt.Inconclusive {
		t.Fatalf("verdict %v %v", r.Verdict, err)
	}
}

func TestVoidLatestStampDoesNotRestoreEarlierStamp(t *testing.T) {
	s, clock, a, first := actionFixture(t)
	ctx := context.Background()
	clock.Add(time.Second)
	seg := segment(t, s, a.LiveSessionID, "I will try again", 10*time.Second)
	evidence := receipt.EvidenceInput{SourceID: seg.ID, Text: seg.Text, Start: seg.Start, End: seg.End, Attribution: seg.Attribution}
	next, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: a.ChannelID, SessionID: a.LiveSessionID, Claim: "Will try again", ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{evidence}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordDetection(ctx, receipt.DetectionInput{ChannelID: a.ChannelID, ReceiptID: next.ID, Evidence: evidence, Outcome: receipt.Stamp}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.VoidOwned(ctx, a, next.ID); err != nil {
		t.Fatal(err)
	}
	current, err := s.CurrentStamp(ctx, a.ChannelID, a.LiveSessionID)
	if err != nil || current != nil {
		t.Fatalf("older stamp resurfaced: %#v %v", current, err)
	}
	if _, err = s.ApplyReceiptAction(ctx, a, first, "old", "admit", "", time.Minute); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("older stamp answer: %v", err)
	}
}

func TestConcurrentDifferentActionsProduceOneAnswer(t *testing.T) {
	s, _, a, id := actionFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, kind := range []string{"admit", "context_changed"} {
		kind := kind
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ApplyReceiptAction(ctx, a, id, kind, kind, "context", time.Minute)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	successes, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("answers: successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestReceiptActionRechecksDurableSession(t *testing.T) {
	s, _, a, id := actionFixture(t)
	ctx := context.Background()
	if _, err := s.db.Writer().ExecContext(ctx, `UPDATE live_sessions SET state='ended',ended_at=? WHERE id=?`, millis(s.now()), a.LiveSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyReceiptAction(ctx, a, id, "stale", "admit", "", time.Minute); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("stale session: %v", err)
	}
}

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"receipts/internal/receipt"
)

func TestJudgmentPairRequiresPersistedCrossSessionChronology(t *testing.T) {
	ctx := context.Background()
	s, clock, c, old := setup(t, Retention{})
	_, err := s.db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','twitch-42','streamer',?,X'00',?,'[]','connected',?)`, c.ID, clock.Now().Add(time.Hour).UnixMilli(), clock.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	original := segment(t, s, old.ID, "I will never play.", time.Second)
	clock.Add(10 * time.Second)
	r, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: old.ID, Claim: original.Text, ClippedBy: "viewer", CreatedAt: clock.Now(), Evidence: []receipt.EvidenceInput{{SourceID: original.ID, Text: original.Text, Start: original.Start, End: original.End, Attribution: original.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Minute)
	current, err := s.StartSession(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	later := segment(t, s, current.ID, "I will play.", time.Second)
	clock.Add(10 * time.Second)
	ev, err := s.Evidence(ctx, r.ID)
	if err != nil || len(ev) != 1 {
		t.Fatalf("evidence=%v err=%v", ev, err)
	}
	a := DetectionAuthority{AccountID: "owner", ChannelID: c.ID, LiveSessionID: current.ID, Generation: 1}
	if err = s.ValidateJudgmentPair(ctx, a, r, ev[0], later); err != nil {
		t.Fatalf("valid prior session: %v", err)
	}
	// A receipt recorded after this line must not be judged against it.
	if _, err = s.db.Writer().ExecContext(ctx, `UPDATE receipts SET created_at=? WHERE id=?`, current.StartedAt.Add(2*time.Second).UnixMilli(), r.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateJudgmentPair(ctx, a, r, ev[0], later); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("future receipt: %v", err)
	}
	// Even a backdated receipt cannot use evidence from a later session clock.
	if _, err = s.db.Writer().ExecContext(ctx, `UPDATE receipts SET created_at=? WHERE id=?`, old.StartedAt.UnixMilli(), r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Writer().ExecContext(ctx, `UPDATE live_sessions SET started_at=? WHERE id=?`, current.StartedAt.Add(time.Minute).UnixMilli(), old.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateJudgmentPair(ctx, a, r, ev[0], later); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("future original evidence: %v", err)
	}
}

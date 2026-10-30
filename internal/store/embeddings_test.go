package store

import (
	"context"
	"errors"
	"receipts/internal/receipt"
	"receipts/internal/retrieval"
	"testing"
	"time"
)

func TestEmbeddingIndexAuthorityAndSpace(t *testing.T) {
	ctx := context.Background()
	s, clock, c, session := setup(t, Retention{})
	now := clock.t
	_, err := s.db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','login',?,X'00',?,'[]','connected',?)`, c.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Writer().ExecContext(ctx, `UPDATE live_sessions SET runtime_generation=1 WHERE id=?`, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	src := segment(t, s, session.ID, "I will never play this map.", 0)
	r, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: session.ID, Claim: src.Text, ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{{SourceID: src.ID, Text: src.Text, Start: src.Start, End: src.End, Attribution: src.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	a := DetectionAuthority{AccountID: "owner", ChannelID: c.ID, LiveSessionID: session.ID, Generation: 1}
	space := retrieval.Space{Model: "fixture", Version: "1", Dimensions: 2}
	entries := []retrieval.Entry{{Receipt: r, Vector: []float64{1, 0}}}
	if err = s.ReindexEmbeddings(ctx, a, space, entries); err != nil {
		t.Fatal(err)
	}
	got, err := s.IndexedReceipts(ctx, a, space)
	if err != nil || len(got) != 1 || got[0].Receipt.ID != r.ID {
		t.Fatalf("index %#v %v", got, err)
	}
	other := a
	other.AccountID = "other"
	if _, err = s.IndexedReceipts(ctx, other, space); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("foreign owner: %v", err)
	}
	other = a
	other.Generation++
	if _, err = s.IndexedReceipts(ctx, other, space); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("generation: %v", err)
	}
	changed := space
	changed.Dimensions = 3
	if _, err = s.IndexedReceipts(ctx, a, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed dimension: %v", err)
	}
	changed = space
	changed.Version = "2"
	if _, err = s.IndexedReceipts(ctx, a, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed version: %v", err)
	}
	if err = s.ReindexEmbeddings(ctx, a, changed, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("incomplete reindex: %v", err)
	}
	_, err = s.db.Writer().ExecContext(ctx, `UPDATE receipts SET validity='void' WHERE id=?`, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.IndexedReceipts(ctx, a, space)
	if err != nil || len(got) != 0 {
		t.Fatalf("void receipt returned: %#v %v", got, err)
	}
}

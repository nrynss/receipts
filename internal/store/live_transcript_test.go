package store

import (
	"context"
	"errors"
	"receipts/internal/receipt"
	"testing"
	"time"
)

func TestLiveTranscriptGenerationAndOverlap(t *testing.T) {
	ctx := context.Background()
	s, _, channel, _ := setup(t, Retention{})
	now := time.Now().UTC()
	_, err := s.db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','login',?,X'00',?,'[]','connected',?)`, channel.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	activation, err := s.StartOwnedLiveSession(ctx, "owner", channel.ID, "broadcast", now, false)
	if err != nil {
		t.Fatal(err)
	}
	seg := receipt.Segment{SessionID: activation.Session.ID, Start: time.Second, End: 2 * time.Second, Text: "words", Attribution: "uncertain", TimingBasis: "coarse_pcm_ingest_hls_delay_unknown"}
	add := func(owner string, generation uint64, seg receipt.Segment) error {
		return s.AddLiveSegment(ctx, owner, channel.ID, activation.Session.ID, generation, seg)
	}
	if err = add("other", activation.Generation, seg); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign owner: %v", err)
	}
	if err = s.ClaimAudioChunk(ctx, "owner", channel.ID, activation.Session.ID, activation.Generation, seg.Start, seg.End); err != nil {
		t.Fatal(err)
	}
	if err = add("owner", activation.Generation, seg); err != nil {
		t.Fatal(err)
	}
	if err = add("owner", activation.Generation, seg); !errors.Is(err, ErrConflict) {
		t.Fatalf("overlap: %v", err)
	}
	if err = s.ResetLiveAuthority(ctx); err != nil {
		t.Fatal(err)
	}
	seg.Start = 2 * time.Second
	seg.End = 3 * time.Second
	if err = add("owner", activation.Generation, seg); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale generation: %v", err)
	}
	resumed, err := s.StartOwnedLiveSession(ctx, "owner", channel.ID, "broadcast", now.Add(time.Second), false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimAudioChunk(ctx, "owner", channel.ID, activation.Session.ID, resumed.Generation, time.Second, 2*time.Second); !errors.Is(err, ErrConflict) {
		t.Fatalf("restart replay: %v", err)
	}
	if err = add("owner", resumed.Generation, seg); err != nil {
		t.Fatal(err)
	}
	segments, err := s.Transcript(ctx, activation.Session.ID)
	if err != nil || len(segments) != 2 {
		t.Fatalf("persisted %d: %v", len(segments), err)
	}
}

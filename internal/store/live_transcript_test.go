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

func TestDetectionTimingSurvivesTranscriptRetention(t *testing.T) {
	s, _, c, session := setup(t, Retention{MaxSegments: 1})
	ctx := context.Background()
	first := segment(t, s, session.ID, "claim", 0)
	r, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: session.ID, Claim: "claim", ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{{SourceID: first.ID, Text: first.Text, Start: first.Start, End: first.End, Attribution: first.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	later := segment(t, s, session.ID, "later speech", 10*time.Second)
	_, err = s.db.Writer().ExecContext(ctx, `UPDATE transcript_segments SET timing_basis='coarse_pcm_ingest_hls_delay_unknown' WHERE id=?`, later.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := receipt.DetectionInput{ChannelID: c.ID, ReceiptID: r.ID, Evidence: receipt.EvidenceInput{SourceID: later.ID, Text: later.Text, Start: later.Start, End: later.End, Attribution: later.Attribution}, Outcome: receipt.NeedsReview}
	d, err := s.RecordDetection(ctx, input)
	if err != nil || d.TimingBasis != "coarse_pcm_ingest_hls_delay_unknown" {
		t.Fatalf("detection provenance: %#v %v", d, err)
	}
	segment(t, s, session.ID, "newer speech", 20*time.Second)
	d, err = s.RecordDetection(ctx, input)
	if err != nil || d.TimingBasis != "coarse_pcm_ingest_hls_delay_unknown" {
		t.Fatalf("retained provenance: %#v %v", d, err)
	}
}

func TestTranscriptionWindowRequiresAdjacentOwnedRawClaims(t *testing.T) {
	ctx := context.Background()
	s, _, channel, _ := setup(t, Retention{})
	now := time.Now().UTC()
	_, err := s.db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','login',?,X'00',?,'[]','connected',?)`, channel.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.StartOwnedLiveSession(ctx, "owner", channel.ID, "broadcast", now, false)
	if err != nil {
		t.Fatal(err)
	}
	claim := func(start, end time.Duration) error {
		return s.ClaimAudioChunk(ctx, "owner", channel.ID, a.Session.ID, a.Generation, start, end)
	}
	window := func(owner string, generation uint64) error {
		return s.ClaimTranscriptionWindow(ctx, owner, channel.ID, a.Session.ID, generation, 0, time.Second, 2*time.Second)
	}
	if err = window("owner", a.Generation); !errors.Is(err, ErrConflict) {
		t.Fatalf("missing roots %v", err)
	}
	if err = claim(0, time.Second); err != nil {
		t.Fatal(err)
	}
	if err = claim(time.Second, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err = window("other", a.Generation); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign window %v", err)
	}
	if err = window("owner", a.Generation+1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale window %v", err)
	}
	seg := receipt.Segment{ID: "first", SessionID: a.Session.ID, Start: 0, End: time.Second, Text: "I promise I", Attribution: "broadcaster", TimingBasis: "coarse_pcm_ingest_hls_delay_unknown"}
	if err = s.AddLiveSegment(ctx, "owner", channel.ID, a.Session.ID, a.Generation, seg); err != nil {
		t.Fatal(err)
	}
	joint := seg
	joint.ID = "joint"
	joint.End = 2 * time.Second
	joint.Text = "I promise I will always pick the blue card."
	if err = s.AddLiveSegment(ctx, "owner", channel.ID, a.Session.ID, a.Generation, joint); !errors.Is(err, ErrConflict) {
		t.Fatalf("unclaimed overlap %v", err)
	}
	if err = window("owner", a.Generation); err != nil {
		t.Fatal(err)
	}
	if err = s.AddLiveSegment(ctx, "owner", channel.ID, a.Session.ID, a.Generation, joint); err != nil {
		t.Fatal(err)
	}
	joint.ID = "duplicate"
	if err = s.AddLiveSegment(ctx, "owner", channel.ID, a.Session.ID, a.Generation, joint); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate window %v", err)
	}
	if err = claim(0, time.Second); !errors.Is(err, ErrConflict) {
		t.Fatalf("raw replay %v", err)
	}
	if err = claim(3*time.Second, 4*time.Second); err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimTranscriptionWindow(ctx, "owner", channel.ID, a.Session.ID, a.Generation, time.Second, 3*time.Second, 4*time.Second); !errors.Is(err, ErrConflict) {
		t.Fatalf("gap joined %v", err)
	}
}

package main

import (
	"context"
	"encoding/binary"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/audio"
	"receipts/internal/auth"
	runtime "receipts/internal/runtime"
	"receipts/internal/store"
)

func TestAudioAttachmentStopsAndIsOwnerScoped(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(ctx, "subject", "owner")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','owner',?,X'00',?,'[]','connected',?)`, channel.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	activation, err := ledger.StartOwnedLiveSession(ctx, "owner", channel.ID, "broadcast", now, false)
	if err != nil {
		t.Fatal(err)
	}
	authority := auth.ChannelContext{AccountID: "owner", ChannelID: channel.ID, LiveSessionID: activation.Session.ID, Generation: activation.Generation}
	manager := runtime.New(runtime.Config{})
	ch, err := manager.StartVerified(ctx, authority, ledger)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	done := make(chan struct{})
	attachment := &audioAttachment{ledger: ledger, runtime: manager, capture: func(ctx context.Context, _, _, _ string, consume func(context.Context, audio.IngestChunk) error) error {
		if err := consume(ctx, audio.IngestChunk{}); err != nil {
			return err
		}
		close(entered)
		<-ctx.Done()
		close(done)
		return ctx.Err()
	}}
	detach, err := attachment.attach(ch.Context, authority, false)
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if !attachment.activeFor(authority) || attachment.activeFor(auth.ChannelContext{AccountID: "other", ChannelID: channel.ID}) {
		t.Fatal("capture status leaked across owners")
	}
	if err = manager.Cancel(authority); err != nil {
		t.Fatal(err)
	}
	detach()
	select {
	case <-done:
	default:
		t.Fatal("source not drained")
	}
	if attachment.activeFor(authority) {
		t.Fatal("capture remained active after stop")
	}
	if _, err = manager.Resolve(authority); !errors.Is(err, runtime.ErrStale) {
		t.Fatalf("runtime remained active: %v", err)
	}
}

type transcriptionFunc func(context.Context, []byte) (string, error)

func (f transcriptionFunc) Transcribe(ctx context.Context, b []byte) (string, error) {
	return f(ctx, b)
}
func TestAudioAttachmentPersistsAttestedSpeech(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(ctx, "subject", "owner")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-5 * time.Second).UTC()
	_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','owner',?,X'00',?,'[]','connected',?)`, channel.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	activation, err := ledger.StartOwnedLiveSession(ctx, "owner", channel.ID, "broadcast", now, false)
	if err != nil {
		t.Fatal(err)
	}
	// The store clock is real; make the session origin precede the recorded audio.
	_, err = db.Writer().ExecContext(ctx, `UPDATE live_sessions SET started_at=? WHERE id=?`, now.UnixMilli(), activation.Session.ID)
	if err != nil {
		t.Fatal(err)
	}
	authority := auth.ChannelContext{AccountID: "owner", ChannelID: channel.ID, LiveSessionID: activation.Session.ID, Generation: activation.Generation}
	manager := runtime.New(runtime.Config{})
	budgets, err := runtime.OpenBudgets(ctx, db, runtime.BudgetConfig{GlobalLimit: 20_000_000, ChannelLimit: 20_000_000})
	if err != nil {
		t.Fatal(err)
	}
	manager.SetBudgets(budgets)
	ch, err := manager.StartVerified(ctx, authority, ledger)
	if err != nil {
		t.Fatal(err)
	}
	complete := make(chan error, 1)
	attachment := &audioAttachment{ledger: ledger, runtime: manager, transcriber: transcriptionFunc(func(context.Context, []byte) (string, error) { return "original speech", nil }), capture: func(ctx context.Context, _, _, _ string, consume func(context.Context, audio.IngestChunk) error) error {
		start := time.UnixMilli(now.UnixMilli()).Add(time.Second)
		err := consume(ctx, audio.IngestChunk{Sequence: 1, Start: start, End: start.Add(time.Second), WAV: oneSecondWAV()})
		complete <- err
		<-ctx.Done()
		return ctx.Err()
	}}
	detach, err := attachment.attach(ch.Context, authority, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-complete; err != nil {
		t.Fatal(err)
	}
	segments, err := ledger.Transcript(ctx, authority.LiveSessionID)
	if err != nil || len(segments) != 1 || segments[0].Text != "original speech" || segments[0].Attribution != "broadcaster" || segments[0].TimingBasis != "coarse_pcm_ingest_hls_delay_unknown" {
		t.Fatalf("transcript: %#v %v", segments, err)
	}
	_ = manager.Cancel(authority)
	detach()
}
func oneSecondWAV() []byte {
	b := make([]byte, 44+32000)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], 32000)
	return b
}

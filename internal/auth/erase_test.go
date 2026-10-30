package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestEraseDisconnectedAccountKeepsOtherOwner(t *testing.T) {
	ctx := context.Background()
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "erase.db"), nil)
	session, _, a := login(t, f.service)
	_, _, err := f.service.IssueDeviceCredential(ctx, ownerContext(f, session, a))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Writer().ExecContext(ctx, `INSERT INTO live_sessions(id,channel_id,started_at,state) VALUES('live',?,1,'live')`, a.ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Writer().ExecContext(ctx, `INSERT INTO audio_chunks(session_id,start_ms,end_ms,generation) VALUES('live',0,1000,1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Writer().ExecContext(ctx, `INSERT INTO transcript_segments(id,session_id,start_ms,end_ms,text,timing_basis,created_at) VALUES('audio-segment','live',0,1000,'words','coarse_pcm_ingest_hls_delay_unknown',1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Writer().ExecContext(ctx, `INSERT INTO receipts(id,channel_id,session_id,created_at,claim,clipped_by,validity) VALUES('receipt',?,'live',1,'claim','viewer','valid')`, a.ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Writer().ExecContext(ctx, `INSERT INTO detections(id,channel_id,receipt_id,session_id,source_id,start_ms,end_ms,evidence_text,attribution,outcome,created_at) VALUES('d',?,'receipt','live','source',0,1,'text','uncertain','stamp',1)`, a.ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Writer().ExecContext(ctx, `INSERT INTO stamp_history(detection_id,channel_id,stamped_at) VALUES('d',?,1)`, a.ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	f.provider.mu.Lock()
	f.provider.grant.UserID = "owner-2"
	f.provider.grant.Login = "other"
	f.provider.mu.Unlock()
	second, _, b := login(t, f.service)
	if err = f.service.RevokeProviderAuthorization(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("cleanup failed")
	if err = f.service.EraseDisconnectedAccount(ctx, a.ID, func(context.Context, string) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("cleanup failure: %v", err)
	}
	called := false
	if err = f.service.EraseDisconnectedAccount(ctx, a.ID, func(_ context.Context, channel string) error { called = channel == a.ChannelID; return nil }); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("wrong channel cleanup")
	}
	var count int
	if err = f.db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE id=?`, a.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("account remains: %d %v", count, err)
	}
	if err = f.db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM audio_chunks WHERE session_id='live'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("audio chunks remain: %d %v", count, err)
	}
	if err = f.db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM transcript_segments WHERE session_id='live'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("audio transcript remains: %d %v", count, err)
	}
	if _, err = f.service.Current(requestWithCookie(second)); err != nil {
		t.Fatalf("other owner lost: %v", err)
	}
	if err = f.db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM channels WHERE id=?`, b.ChannelID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("other channel lost: %d %v", count, err)
	}
}

func requestWithCookie(c *http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	return r
}

func TestDeletionRejectsOAuthFlowStartedBeforeErasure(t *testing.T) {
	ctx := context.Background()
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "barrier.db"), nil)
	_, _, a := login(t, f.service)
	started := f.clock.Now().UnixMilli()
	expiry := f.clock.Now().Add(f.service.stateTTL).UnixMilli()
	if err := f.service.RevokeProviderAuthorization(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.EraseDisconnectedAccount(ctx, a.ID, func(context.Context, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	sealed, err := f.service.seal(a.TwitchID, storedTokens{Access: "fixture", Refresh: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	grant := f.provider.grant
	if _, err = f.service.upsertAccountFromState(ctx, grant, sealed, time.Hour, started, expiry); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("stale callback: %v", err)
	}
	f.clock.Add(time.Millisecond)
	_, _, fresh := login(t, f.service)
	if fresh.ID == a.ID {
		t.Fatal("new sign-in reused deleted account")
	}
}

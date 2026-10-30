//go:build live

package session_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/audio"
	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/session"
	"receipts/internal/store"
	"receipts/internal/twitch"
	"receipts/internal/twitch/connection"
)

type liveResolver struct{ grant connection.Grant }

func (r liveResolver) Resolve(context.Context, auth.ChannelContext) (connection.Grant, error) {
	return r.grant, nil
}

type liveToken string

func (t liveToken) AccessToken(context.Context, string) (string, error) { return string(t), nil }
func (liveToken) RefreshAfterUnauthorized(context.Context, string, string) (string, error) {
	return "", connection.ErrLost
}

type liveTransport struct{ calls atomic.Int32 }

func (r *liveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if r.calls.Add(1) > 20 {
		return nil, errors.New("live status request budget exhausted")
	}
	return http.DefaultTransport.RoundTrip(req)
}

// TestLiveAuthorityStopsCapture binds a real ephemeral capture subprocess to
// the production authority service. It never transcribes, posts chat or clips.
func TestLiveAuthorityStopsCapture(t *testing.T) {
	if os.Getenv("RECEIPTS_LIVE_AUTHORITY") != "1" {
		t.Skip("explicit live authority opt-in required")
	}
	streamlink := os.Getenv("RECEIPTS_STREAMLINK")
	if streamlink == "" {
		streamlink = "/tmp/receipts-audio-probe/bin/streamlink"
	}
	if _, err := exec.LookPath(streamlink); err != nil {
		t.Fatal("streamlink probe executable unavailable")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Fatal("ffmpeg unavailable")
	}
	clientID, token, broadcaster := os.Getenv("TWITCH_CLIENT_ID"), os.Getenv("TWITCH_ACCESS_TOKEN"), os.Getenv("TWITCH_BROADCASTER_ID")
	if clientID == "" || token == "" || broadcaster == "" {
		t.Fatal("Twitch connection variables are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	transport := &liveTransport{}
	httpClient := &http.Client{Timeout: 5 * time.Second, Transport: transport}
	grant, err := (twitch.Client{ClientID: clientID, Token: token, HTTP: httpClient}).Validate(ctx)
	if err != nil {
		t.Fatal("Twitch token validation failed")
	}
	if grant.ClientID != clientID || grant.UserID != broadcaster || !strings.EqualFold(grant.Login, "nrynss") {
		t.Fatal("approved broadcaster identity mismatch")
	}
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "authority.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(ctx, broadcaster, "live-acceptance")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('live-authority-owner','twitch',?,'live-acceptance',?,X'01',0,'','connected',0)`, broadcaster, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	manager := channelruntime.New(channelruntime.Config{MaxChannels: 1})
	defer manager.StopAccount("live-authority-owner")
	adapter := &connection.Adapter{Runtime: manager, Resolver: liveResolver{connection.Grant{ClientID: clientID, SubjectID: broadcaster, ChannelPlatformID: broadcaster, Scopes: grant.Scopes}}, Tokens: liveToken(token), HTTP: httpClient}
	captureResult := make(chan error, 1)
	output := filepath.Join(t.TempDir(), "discarded-audio")
	var active *channelruntime.Channel
	authority := &session.Authority{Adapter: adapter, Ledger: ledger, Runtime: manager, Root: ctx, Freshness: 15 * time.Second, PollInterval: 5 * time.Second}
	authority.Attach = func(_ context.Context, a auth.ChannelContext) (func(), error) {
		var resolveErr error
		active, resolveErr = manager.Resolve(a)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if !active.Go(func(sourceCtx context.Context) {
			captureResult <- audio.CaptureUntilCanceled(sourceCtx, streamlink, "https://www.twitch.tv/nrynss", output)
		}) {
			return nil, errors.New("capture registration failed")
		}
		return nil, nil
	}
	if err := authority.Start(ctx, auth.ChannelContext{AccountID: "live-authority-owner", ChannelID: channel.ID}); err != nil {
		t.Fatal("confirmed live activation failed; no capture proof")
	}
	readyTimeout := time.NewTimer(40 * time.Second)
	defer readyTimeout.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	ready := false
	for !ready {
		select {
		case <-tick.C:
			paths, _ := filepath.Glob(filepath.Join(output, "chunk-*.wav"))
			for _, path := range paths {
				if info, e := os.Stat(path); e == nil && info.Size() >= 32044 {
					ready = true
					break
				}
			}
		case <-active.Context.Done():
			t.Fatal("authority stopped before real capture became ready")
		case <-captureResult:
			t.Fatal("real capture exited before readiness; no capture-stop proof")
		case <-readyTimeout.C:
			t.Fatal("real capture produced no sample data within budget")
		case <-ctx.Done():
			t.Fatal("live capture startup deadline exceeded")
		}
	}
	t.Log("READY: real capture has sample data; stop the nrynss test stream now")
	select {
	case <-active.Context.Done():
	case <-ctx.Done():
		t.Fatal("authority did not stop within live budget")
	}
	select {
	case <-captureResult:
	case <-time.After(8 * time.Second):
		t.Fatal("capture subprocesses did not drain after authority cancellation")
	}
	_, _, _, _, _, persistedErr := ledger.CurrentLiveAuthority(context.Background(), "live-authority-owner", channel.ID)
	if !errors.Is(persistedErr, store.ErrNotFound) {
		t.Fatal("authority stopped without a confirmed offline/ended ledger session")
	}
	t.Logf("PASS: real capture exited and confirmed broadcast session ended; Twitch requests=%d; no model calls; temporary audio discarded", transport.calls.Load())
}

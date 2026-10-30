//go:build live

package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/audio"
	"receipts/internal/auth"
	runtime "receipts/internal/runtime"
	"receipts/internal/session"
	"receipts/internal/store"
	"receipts/internal/transcribe"
	"receipts/internal/twitch"
	"receipts/internal/twitch/connection"
)

type boundedTwitchTransport struct{ calls atomic.Int32 }

func (b *boundedTwitchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if b.calls.Add(1) > 30 {
		return nil, errors.New("live: Twitch request limit reached")
	}
	return http.DefaultTransport.RoundTrip(req)
}

type liveAudioResolver struct{ grant connection.Grant }

func (r liveAudioResolver) Resolve(context.Context, auth.ChannelContext) (connection.Grant, error) {
	return r.grant, nil
}

type liveAudioToken string

func (t liveAudioToken) AccessToken(context.Context, string) (string, error) { return string(t), nil }
func (liveAudioToken) RefreshAfterUnauthorized(context.Context, string, string) (string, error) {
	return "", connection.ErrLost
}

type oneLiveTranscription struct {
	client transcribe.Client
	calls  atomic.Int32
}

func (o *oneLiveTranscription) Transcribe(ctx context.Context, b []byte) (string, error) {
	if o.calls.Add(1) != 1 {
		return "", errors.New("live: single-call limit reached")
	}
	return o.client.Transcribe(ctx, b)
}

// TestLiveBroadcastTranscription is one opt-in paid case. No chat or clips are
// created; the SQLite database and captured audio are temporary. The channel
// and installation ceilings are each $0.02, and exactly one paid OpenAI request is
// allowed. The owner must broadcast original solo speech and then stop it.
func TestLiveBroadcastTranscription(t *testing.T) {
	if os.Getenv("RECEIPTS_LIVE_TRANSCRIPTION") != "1" {
		t.Skip("explicit live transcription opt-in required")
	}
	need := func(name string) string {
		v := os.Getenv(name)
		if v == "" {
			t.Fatalf("missing %s", name)
		}
		return v
	}
	clientID, token, broadcaster, key := need("TWITCH_CLIENT_ID"), need("TWITCH_ACCESS_TOKEN"), need("TWITCH_BROADCASTER_ID"), need("OPENAI_API_KEY")
	streamlink := need("RECEIPTS_STREAMLINK_BIN")
	expected := strings.ToLower(need("RECEIPTS_LIVE_EXPECTED_PHRASE"))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	twitchTransport := &boundedTwitchTransport{}
	httpClient := &http.Client{Timeout: 5 * time.Second, Transport: twitchTransport}
	grant, err := (twitch.Client{ClientID: clientID, Token: token, HTTP: httpClient}).Validate(ctx)
	if err != nil || grant.UserID != broadcaster || grant.ClientID != clientID || !strings.EqualFold(grant.Login, "nrynss") {
		t.Fatal("authorized broadcaster validation failed")
	}
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "live.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(ctx, broadcaster, grant.Login)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('live-owner','twitch',?,?,?,X'00',0,'[]','connected',0)`, broadcaster, grant.Login, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	manager := runtime.New(runtime.Config{MaxChannels: 1})
	defer manager.StopAccount("live-owner")
	budgets, err := runtime.OpenBudgets(ctx, db, runtime.BudgetConfig{GlobalLimit: 20_000_000, ChannelLimit: 20_000_000})
	if err != nil {
		t.Fatal(err)
	}
	manager.SetBudgets(budgets)
	adapter := &connection.Adapter{Runtime: manager, Resolver: liveAudioResolver{connection.Grant{ClientID: clientID, SubjectID: broadcaster, ChannelPlatformID: broadcaster, Scopes: grant.Scopes}}, Tokens: liveAudioToken(token), HTTP: httpClient}
	provider := &oneLiveTranscription{client: transcribe.Client{APIKey: key}}
	capture := &audioAttachment{ledger: ledger, runtime: manager, apiKey: key, streamlink: streamlink, transcriber: provider}
	capture.capture = func(ctx context.Context, bin, url, dir string, consume func(context.Context, audio.IngestChunk) error) error {
		used := false
		return audio.StreamChunks(ctx, bin, url, dir, func(ctx context.Context, chunk audio.IngestChunk) error {
			if used {
				return nil
			}
			used = true
			return consume(ctx, chunk)
		})
	}
	authority := &session.Authority{Adapter: adapter, Ledger: ledger, Runtime: manager, Root: ctx, Freshness: 15 * time.Second, PollInterval: 5 * time.Second, AttachAudio: capture.attach, AudioActive: capture.activeFor}
	identity := auth.ChannelContext{AccountID: "live-owner", ChannelID: channel.ID}
	if err = authority.StartWithOptions(ctx, identity, false, true); err != nil {
		t.Fatal("confirmed live start failed")
	}
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = authority.Stop(stopCtx, identity)
	}()
	t.Log("READY: continue repeating the original solo phrase until transcript is observed")
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var sessionID string
	var observed bool
	for !observed {
		select {
		case <-ctx.Done():
			t.Fatal("no final transcript before deadline")
		case <-ticker.C:
			id, _, _, _, _, e := ledger.CurrentLiveAuthority(ctx, "live-owner", channel.ID)
			if e != nil {
				t.Fatal("session ended before transcript")
			}
			sessionID = id
			segments, e := ledger.Transcript(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			if len(segments) > 0 {
				s := segments[0]
				if s.Attribution != "broadcaster" || s.TimingBasis != "coarse_pcm_ingest_hls_delay_unknown" || s.End <= s.Start || !strings.Contains(strings.ToLower(s.Text), expected) {
					t.Fatal("invalid transcript evidence")
				}
				observed = true
			}
		}
	}
	if provider.calls.Load() != 1 {
		t.Fatal("provider call count exceeded one")
	}
	t.Log("TRANSCRIPT_READY: stop the stream now; audio offsets are coarse ingest time with unknown HLS delay")
	for {
		select {
		case <-ctx.Done():
			t.Fatal("session did not stop before deadline")
		case <-ticker.C:
			if _, _, _, _, _, e := ledger.CurrentLiveAuthority(ctx, "live-owner", channel.ID); errors.Is(e, store.ErrNotFound) || manager.Count() == 0 {
				rows, e := ledger.Transcript(ctx, sessionID)
				if e != nil || len(rows) != 1 {
					t.Fatal("unexpected final transcript count")
				}
				if provider.calls.Load() != 1 {
					t.Fatal("more than one provider attempt")
				}
				accountCtx := auth.WithChannelContext(context.Background(), authorityIdentity(identity, sessionID))
				_, global, e := budgets.Remaining(accountCtx, channel.ID)
				if e != nil {
					t.Fatal("budget read failed")
				}
				charged := int64(20_000_000 - global)
				if charged <= 0 || charged > 4_500_000 {
					t.Fatal("charged estimate exceeded one call")
				}
				t.Logf("PASS: one paid attempt, charged estimate $%.6f, Twitch requests %d, expected attributed coarse ingest evidence, and offline or source-failure stop", float64(charged)/1e9, twitchTransport.calls.Load())
				return
			}
		}
	}
}

func authorityIdentity(identity auth.ChannelContext, sessionID string) auth.ChannelContext {
	identity.LiveSessionID = sessionID
	return identity
}

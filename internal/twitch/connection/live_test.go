//go:build live

package connection

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/twitch"
)

// This check is deliberately read-only: one token validation and one stream
// lookup, with no chat delivery, clips, subscriptions, or model spend.
func TestLiveConnectionCapability(t *testing.T) {
	if os.Getenv("RECEIPTS_LIVE_CONNECTION") != "1" {
		t.Skip("set RECEIPTS_LIVE_CONNECTION=1 to opt in")
	}
	clientID, token, broadcasterID := os.Getenv("TWITCH_CLIENT_ID"), os.Getenv("TWITCH_ACCESS_TOKEN"), os.Getenv("TWITCH_BROADCASTER_ID")
	if clientID == "" || token == "" || broadcasterID == "" {
		t.Fatal("live connection variables are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := twitch.Client{ClientID: clientID, Token: token, HTTP: &http.Client{Timeout: 5 * time.Second}}
	grant, err := client.Validate(ctx)
	if err != nil {
		t.Fatalf("token validation failed: %v", err)
	}
	if grant.ClientID != clientID || grant.UserID != broadcasterID || !strings.EqualFold(grant.Login, "nrynss") {
		t.Fatal("token client or broadcaster identity mismatch")
	}
	for _, scope := range RequiredScopes() {
		if !has(grant.Scopes, scope) {
			t.Fatalf("required capability is missing: %s", scope)
		}
	}
	if _, err := client.StreamLive(ctx, broadcasterID); err != nil {
		t.Fatalf("stream lookup failed: %v", err)
	}
}

// TestLiveAdapterWrites is an explicitly opted-in, bounded adapter-level
// capability check. It validates the grant and confirms the broadcaster is
// live before sending exactly one fixed chat message and creating one clip.
func TestLiveAdapterWrites(t *testing.T) {
	if os.Getenv("RECEIPTS_LIVE_ADAPTER_WRITES") != "1" {
		t.Skip("set RECEIPTS_LIVE_ADAPTER_WRITES=1 to opt in to one chat write and one clip")
	}
	clientID, token, broadcasterID := os.Getenv("TWITCH_CLIENT_ID"), os.Getenv("TWITCH_ACCESS_TOKEN"), os.Getenv("TWITCH_BROADCASTER_ID")
	if clientID == "" || token == "" || broadcasterID == "" {
		t.Fatal("live connection variables are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	counter := &liveRequestCounter{limit: 8}
	httpClient := &http.Client{Timeout: 8 * time.Second, Transport: counter}
	client := twitch.Client{ClientID: clientID, Token: token, HTTP: httpClient}
	grant, err := client.Validate(ctx)
	if err != nil {
		t.Fatalf("token validation failed: %v", err)
	}
	if grant.ClientID != clientID || grant.UserID != broadcasterID || !strings.EqualFold(grant.Login, "nrynss") {
		t.Fatal("token client or broadcaster identity mismatch")
	}
	for _, scope := range RequiredScopes() {
		if !has(grant.Scopes, scope) {
			t.Fatalf("required capability is missing: %s", scope)
		}
	}

	authority := auth.ChannelContext{AccountID: "live-acceptance", ChannelID: "live-acceptance", LiveSessionID: "live-acceptance", Generation: 1}
	runtime := channelruntime.New(channelruntime.Config{MaxChannels: 1})
	if _, err := runtime.Start(ctx, authority); err != nil {
		t.Fatalf("start test runtime: %v", err)
	}
	defer runtime.Stop(authority)
	resolver := resolverFunc(func(context.Context, auth.ChannelContext) (Grant, error) {
		return Grant{ClientID: clientID, SubjectID: broadcasterID, ChannelPlatformID: broadcasterID, Scopes: grant.Scopes}, nil
	})
	tokens := liveTokens{token: token}
	adapter := Adapter{Runtime: runtime, Resolver: resolver, Tokens: tokens, HTTP: httpClient}
	live, err := adapter.StreamLive(ctx, authority)
	if err != nil {
		t.Fatalf("adapter stream check failed: %v", err)
	}
	if !live {
		t.Fatal("broadcaster is offline; no chat or clip write was attempted")
	}
	sent, err := adapter.SendChatMessage(ctx, authority, "Receipts capability test")
	if err != nil {
		t.Fatalf("adapter chat send failed: %v", err)
	}
	if !sent {
		t.Fatal("Twitch did not accept the capability test message")
	}
	clipID, err := adapter.CreateClip(ctx, authority)
	if err != nil {
		t.Fatalf("adapter clip creation failed: %v", err)
	}
	if clipID == "" {
		t.Fatal("adapter returned an empty clip ID")
	}
	if got := counter.calls.Load(); got > counter.limit {
		t.Fatalf("live request budget exceeded: %d > %d", got, counter.limit)
	}
	t.Logf("validated broadcaster identity; adapter stream lookup was live; one chat message accepted; one clip ID returned; HTTP requests=%d", counter.calls.Load())
}

type liveTokens struct{ token string }

func (t liveTokens) AccessToken(context.Context, string) (string, error) { return t.token, nil }
func (t liveTokens) RefreshAfterUnauthorized(context.Context, string, string) (string, error) {
	return "", errors.New("live acceptance does not refresh credentials")
}

type liveRequestCounter struct {
	calls atomic.Int32
	limit int32
}

func (c *liveRequestCounter) RoundTrip(r *http.Request) (*http.Response, error) {
	if c.calls.Add(1) > c.limit {
		return nil, errors.New("live acceptance request budget exceeded")
	}
	return http.DefaultTransport.RoundTrip(r)
}

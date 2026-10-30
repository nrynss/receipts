//go:build live

package connection

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/twitch"
)

// TestLiveClipReadiness reuses an existing clip ID and performs no writes.
func TestLiveClipReadiness(t *testing.T) {
	if os.Getenv("RECEIPTS_LIVE_CLIP_READINESS") != "1" {
		t.Skip("set RECEIPTS_LIVE_CLIP_READINESS=1 to check one existing clip")
	}
	clientID, token, broadcasterID, clipID := os.Getenv("TWITCH_CLIENT_ID"), os.Getenv("TWITCH_ACCESS_TOKEN"), os.Getenv("TWITCH_BROADCASTER_ID"), os.Getenv("RECEIPTS_LIVE_CLIP_ID")
	if clientID == "" || token == "" || broadcasterID == "" || clipID == "" {
		t.Fatal("live clip readiness variables are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	counter := &liveRequestCounter{limit: 3}
	httpClient := &http.Client{Timeout: 5 * time.Second, Transport: counter}
	client := twitch.Client{ClientID: clientID, Token: token, HTTP: httpClient}
	grant, err := client.Validate(ctx)
	if err != nil {
		t.Fatalf("token validation failed: %v", err)
	}
	if grant.ClientID != clientID || grant.UserID != broadcasterID || !strings.EqualFold(grant.Login, "nrynss") {
		t.Fatal("token client or broadcaster identity mismatch")
	}
	a := auth.ChannelContext{AccountID: "live-readiness", ChannelID: "live-readiness", LiveSessionID: "live-readiness", Generation: 1}
	manager := channelruntime.New(channelruntime.Config{MaxChannels: 1})
	if _, err = manager.Start(ctx, a); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(a)
	adapter := Adapter{Runtime: manager, Resolver: resolverFunc(func(context.Context, auth.ChannelContext) (Grant, error) {
		return Grant{ClientID: clientID, SubjectID: broadcasterID, ChannelPlatformID: broadcasterID, Scopes: grant.Scopes}, nil
	}), Tokens: liveTokens{token: token}, HTTP: httpClient}
	readiness, err := adapter.GetClip(ctx, a, clipID)
	if err != nil {
		t.Fatalf("read-only clip readiness lookup failed: %v", err)
	}
	if !readiness.Ready {
		t.Fatal("existing clip is not yet visible through Get Clips")
	}
	if counter.calls.Load() > counter.limit {
		t.Fatalf("live request budget exceeded: %d", counter.calls.Load())
	}
	t.Logf("existing clip readiness verified with %d requests; clip ID and URL omitted", counter.calls.Load())
}

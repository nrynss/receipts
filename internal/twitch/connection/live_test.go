//go:build live

package connection

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

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
	if grant.ClientID != clientID || grant.UserID != broadcasterID {
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

//go:build live

package twitch

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveChannelProbe performs a single explicitly enabled integration case.
// Budget: no model spend, one public reply, one clip, at most 20 HTTP requests
// (21 with the optional single self-trigger chat message),
// and a 90-second deadline. Clip playback still requires an OBS-origin check.
func TestLiveChannelProbe(t *testing.T) {
	if os.Getenv("RECEIPTS_LIVE_PROBE") != "1" {
		t.Skip("set RECEIPTS_LIVE_PROBE=1 to enable the bounded own-channel probe")
	}
	required := func(name string) string {
		t.Helper()
		value := os.Getenv(name)
		if value == "" {
			t.Fatalf("missing %s", name)
		}
		return value
	}
	client := Client{ClientID: required("TWITCH_CLIENT_ID"), Token: required("TWITCH_ACCESS_TOKEN")}
	broadcaster := required("TWITCH_BROADCASTER_ID")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	identity, err := client.Validate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if identity.ClientID != client.ClientID || identity.UserID != broadcaster || !strings.EqualFold(identity.Login, "nrynss") {
		t.Fatal("probe requires the designated nrynss broadcaster's own user token and matching app")
	}
	for _, scope := range []string{"user:read:chat", "user:write:chat", "clips:edit"} {
		found := false
		for _, granted := range identity.Scopes {
			found = found || granted == scope
		}
		if !found {
			t.Fatalf("user token is missing scope %s", scope)
		}
	}
	live, err := client.StreamLive(ctx, broadcaster)
	if err != nil {
		t.Fatal(err)
	}
	if !live {
		t.Fatal("nrynss must be live with original content before running this probe")
	}
	socket, err := DialEventSub(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	frame, err := socket.Next(ctx)
	if err != nil {
		t.Fatal("EventSub welcome could not be read")
	}
	welcome, err := ParseEventSub(frame)
	if err != nil || welcome.Type != "session_welcome" || welcome.SessionID == "" {
		t.Fatal("expected a valid EventSub welcome")
	}
	if _, err := client.SubscribeChat(ctx, broadcaster, identity.UserID, welcome.SessionID); err != nil {
		t.Fatal(err)
	}
	t.Log("Subscribed. Say an original promise on stream, then type !receipt probe in nrynss chat.")
	if os.Getenv("RECEIPTS_PROBE_SELF_TRIGGER") == "1" {
		sent, err := client.SendChatMessage(ctx, ChatMessage{BroadcasterID: broadcaster, SenderID: identity.UserID, Message: "!receipt probe"})
		if err != nil || !sent {
			t.Fatal("Twitch did not accept the single self-trigger message")
		}
		t.Log("Published the single self-trigger command; waiting for its EventSub notification.")
	}
	var received time.Time
	for received.IsZero() {
		frame, err := socket.Next(ctx)
		if err != nil {
			t.Fatal("EventSub read failed or probe deadline expired")
		}
		message, err := ParseEventSub(frame)
		if err != nil {
			t.Fatal(err)
		}
		switch message.Type {
		case "session_keepalive":
			continue
		case "notification":
			var event struct {
				Broadcaster string `json:"broadcaster_user_id"`
				Message     struct {
					Text string `json:"text"`
				} `json:"message"`
			}
			if json.Unmarshal(message.Event, &event) != nil {
				t.Fatal("invalid chat notification")
			}
			if event.Broadcaster == broadcaster && strings.TrimSpace(event.Message.Text) == "!receipt probe" {
				received = time.Now().UTC()
			}
		default:
			t.Fatal("EventSub session changed; rerun the bounded probe rather than silently reconnecting")
		}
	}
	sent, err := client.SendChatMessage(ctx, ChatMessage{BroadcasterID: broadcaster, SenderID: identity.UserID, Message: "Receipts connection probe: chat received; checking clip creation."})
	if err != nil || !sent {
		t.Fatal("Twitch did not accept the single probe reply")
	}
	replied := time.Now().UTC()
	clipID, err := client.CreateClip(ctx, broadcaster)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC()
	var clip Clip
	var ready time.Time
	pollCtx, stopPolling := context.WithTimeout(ctx, 15*time.Second)
	defer stopPolling()
	for attempt := 0; attempt < 15; attempt++ {
		result, found, err := client.GetClip(pollCtx, clipID)
		if err != nil {
			t.Fatal(err)
		}
		if found {
			clip, ready = result, time.Now().UTC()
			break
		}
		select {
		case <-pollCtx.Done():
			t.Fatal("clip not ready within the bounded polling window")
		case <-time.After(time.Second):
		}
	}
	if ready.IsZero() || clip.EmbedURL == "" {
		t.Fatal("clip did not become ready with an embed URL")
	}
	evidence := struct {
		Channel                           string `json:"channel"`
		Received, Replied, Created, Ready time.Time
		ClipID                            string `json:"clip_id"`
		EmbedURL                          string `json:"embed_url"`
		PlaybackVerified                  bool   `json:"playback_verified"`
	}{Channel: "nrynss", Received: received, Replied: replied, Created: created, Ready: ready, ClipID: clip.ID, EmbedURL: clip.EmbedURL}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if output := os.Getenv("RECEIPTS_PROBE_OUTPUT"); output != "" {
		if err := os.WriteFile(output, append(data, '\n'), 0o600); err != nil {
			t.Fatal("could not write sanitized probe evidence")
		}
	}
	t.Logf("Probe passed: one reply, one clip; readiness latency %s. OBS playback remains unverified.", ready.Sub(created))
}

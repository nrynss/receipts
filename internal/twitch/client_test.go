package twitch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestClientHelixRequestsAndParsesResponses(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Client-Id") != "client-test" || r.Header.Get("Authorization") != "Bearer token-test" {
			t.Error("missing Helix authorization headers")
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /streams":
			if r.URL.Query().Get("user_id") != "broadcaster-1" {
				t.Error("wrong live-status user")
			}
			return response(200, `{"data":[{"id":"stream-1"}]}`), nil
		case "POST /chat/messages":
			var msg ChatMessage
			if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
				t.Error(err)
			}
			if msg.Message == "" {
				t.Error("empty chat message")
			}
			return response(200, `{"data":[{"is_sent":true}]}`), nil
		case "POST /clips":
			return response(200, `{"data":[{"id":"clip-1"}]}`), nil
		case "GET /clips":
			return response(200, `{"data":[{"id":"clip-1","embed_url":"https://clips.twitch.tv/embed?clip=clip-1"}]}`), nil
		case "POST /eventsub/subscriptions":
			return response(200, `{"data":[{"status":"enabled"}]}`), nil
		default:
			return response(404, "not found"), nil
		}
	})
	c := Client{ClientID: "client-test", Token: "token-test", HTTP: &http.Client{Transport: transport}, BaseURL: "https://unit.invalid"}
	ctx := context.Background()
	live, err := c.StreamLive(ctx, "broadcaster-1")
	if err != nil || !live {
		t.Fatalf("StreamLive = %v, %v", live, err)
	}
	sent, err := c.SendChatMessage(ctx, ChatMessage{BroadcasterID: "broadcaster-1", SenderID: "sender-1", Message: "probe"})
	if err != nil || !sent {
		t.Fatalf("SendChatMessage = %v, %v", sent, err)
	}
	clipID, err := c.CreateClip(ctx, "broadcaster-1")
	if err != nil || clipID != "clip-1" {
		t.Fatalf("CreateClip = %q, %v", clipID, err)
	}
	clip, found, err := c.GetClip(ctx, clipID)
	if err != nil || !found || clip.EmbedURL == "" {
		t.Fatalf("GetClip = %#v, %v, %v", clip, found, err)
	}
	sub, err := c.SubscribeChat(ctx, "broadcaster-1", "sender-1", "session-1")
	if err != nil || sub.Status != "enabled" {
		t.Fatalf("SubscribeChat = %#v, %v", sub, err)
	}
}

func TestClientDoesNotExposeProviderErrorBody(t *testing.T) {
	transport := roundTripFunc(func(_ *http.Request) (*http.Response, error) { return response(401, "secret-token-echo"), nil })
	c := Client{ClientID: "client", Token: "secret-token", HTTP: &http.Client{Transport: transport}, BaseURL: "https://unit.invalid"}
	_, err := c.StreamLive(context.Background(), "broadcaster")
	if err == nil {
		t.Fatal("expected request failure")
	}
	if got := err.Error(); got == "" || strings.Contains(got, "secret-token") || strings.Contains(got, "secret-token-echo") {
		t.Fatalf("error leaked sensitive text: %s", got)
	}
}

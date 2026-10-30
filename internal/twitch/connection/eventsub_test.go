package connection

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
)

func TestSubscribeChatVerifiedBindsGrantAndRequiresEnabled(t *testing.T) {
	a := auth.ChannelContext{AccountID: "owner", ChannelID: "local", LiveSessionID: "live", Generation: 1}
	runtime := channelruntime.New(channelruntime.Config{})
	if _, err := runtime.Start(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	var calls int
	adapter := Adapter{Runtime: runtime, Resolver: resolverFunc(func(_ context.Context, got auth.ChannelContext) (Grant, error) {
		if got.ChannelID != "local" {
			t.Fatal("wrong channel authority")
		}
		return Grant{ClientID: "client", SubjectID: "twitch-owner", ChannelPlatformID: "twitch-owner", Scopes: RequiredScopes()}, nil
	}), Tokens: &tokenFixture{}, BaseURL: "https://unit.invalid", HTTP: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/eventsub/subscriptions" {
			t.Errorf("path=%s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		var body struct {
			Type      string            `json:"type"`
			Condition map[string]string `json:"condition"`
			Transport map[string]string `json:"transport"`
		}
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatal(err)
		}
		if body.Type != "channel.chat.message" || body.Condition["broadcaster_user_id"] != "twitch-owner" || body.Condition["user_id"] != "twitch-owner" || body.Transport["session_id"] != "socket-session" {
			t.Errorf("subscription body=%s", b)
		}
		if r.Header.Get("Authorization") != "Bearer access-a" {
			t.Error("missing bearer authorization")
		}
		return response(http.StatusAccepted, `{"data":[{"status":"enabled"}]}`), nil
	})}}
	if err := adapter.SubscribeChatVerified(context.Background(), a, "socket-session"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("requests=%d", calls)
	}
}

func TestSubscribeChatVerifiedRejectsDisabledAndForbidden(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{{"disabled", http.StatusAccepted, `{"data":[{"status":"websocket_disconnected"}]}`, nil}, {"forbidden", http.StatusForbidden, `{}`, ErrBinding}} {
		t.Run(tc.name, func(t *testing.T) {
			a := auth.ChannelContext{AccountID: "owner", ChannelID: "local", LiveSessionID: "live", Generation: 1}
			runtime := channelruntime.New(channelruntime.Config{})
			_, _ = runtime.Start(context.Background(), a)
			adapter := Adapter{Runtime: runtime, Resolver: resolverFunc(func(context.Context, auth.ChannelContext) (Grant, error) {
				return Grant{ClientID: "client", SubjectID: "twitch", ChannelPlatformID: "twitch", Scopes: RequiredScopes()}, nil
			}), Tokens: &tokenFixture{}, BaseURL: "https://unit.invalid", HTTP: &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) { return response(tc.status, tc.body), nil })}}
			err := adapter.SubscribeChatVerified(context.Background(), a, "session")
			if err == nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error=%v", err)
			}
			if strings.Contains(err.Error(), "access-a") {
				t.Fatal("token leaked")
			}
		})
	}
}

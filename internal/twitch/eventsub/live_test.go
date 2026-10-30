//go:build live

package eventsub

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"receipts/internal/auth"
	"receipts/internal/twitch"
	"receipts/internal/twitch/connection"
)

type envResolver struct{ grant connection.Grant }

func (r envResolver) Resolve(context.Context, auth.ChannelContext) (connection.Grant, error) {
	return r.grant, nil
}

type envToken string

func (t envToken) AccessToken(context.Context, string) (string, error) { return string(t), nil }
func (t envToken) RefreshAfterUnauthorized(context.Context, string, string) (string, error) {
	return "", connection.ErrLost
}

type countedTransport struct {
	calls atomic.Int32
	base  http.RoundTripper
}

func (t *countedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return t.base.RoundTrip(r)
}

// This receive-only acceptance creates one temporary subscription and uses a
// synthetic persisted runtime; it never posts a chat reply or creates a clip.
func TestLiveEventSubTransport(t *testing.T) {
	if os.Getenv("RECEIPTS_EVENTSUB_LIVE") != "1" {
		t.Skip("set RECEIPTS_EVENTSUB_LIVE=1 to opt in")
	}
	clientID, token, broadcaster := os.Getenv("TWITCH_CLIENT_ID"), os.Getenv("TWITCH_ACCESS_TOKEN"), os.Getenv("TWITCH_BROADCASTER_ID")
	if clientID == "" || token == "" || broadcaster == "" {
		t.Fatal("Twitch client, access token, and broadcaster ID environment variables are required")
	}
	prepCtx, prepCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer prepCancel()
	transport := &countedTransport{base: http.DefaultTransport}
	httpClient := &http.Client{Timeout: 8 * time.Second, Transport: transport}
	validated, err := (twitch.Client{ClientID: clientID, Token: token, HTTP: httpClient}).Validate(prepCtx)
	if err != nil {
		t.Fatalf("token validation failed: %v", err)
	}
	if validated.ClientID != clientID || validated.UserID != broadcaster || !strings.EqualFold(validated.Login, "nrynss") {
		t.Fatal("approved Twitch identity does not match nrynss broadcaster")
	}
	readScope := false
	for _, scope := range validated.Scopes {
		if scope == "user:read:chat" {
			readScope = true
		}
	}
	if !readScope {
		t.Fatal("authorized Twitch token lacks user:read:chat")
	}
	runCtx, stopRun := context.WithCancel(context.Background())
	defer stopRun()
	f := newAcceptanceFixture(t, runCtx, broadcaster)
	manager := f.manager
	resolver := envResolver{grant: connection.Grant{ClientID: clientID, SubjectID: validated.UserID, ChannelPlatformID: broadcaster, Scopes: validated.Scopes}}
	conn := &connection.Adapter{Runtime: manager, Resolver: resolver, Tokens: envToken(token), HTTP: httpClient}
	ready := make(chan struct{})
	var readyOnce sync.Once
	retry := make(chan error, 1)
	c := &Client{Helix: Adapter{Connection: conn}, Sink: f.sink, MinBackoff: time.Second, MaxBackoff: 2 * time.Second, OnSubscribed: func() { readyOnce.Do(func() { close(ready) }) }, OnRetry: func(err error, _ time.Duration) {
		retry <- errors.New("EventSub transport failed; retry suppressed to cap the live request count")
		stopRun()
	}}
	done := make(chan error, 1)
	go func() { done <- c.Run(runCtx, f.authority) }()
	select {
	case <-ready:
		t.Log("READY: temporary chat subscription is active; type the requested commands")
	case err := <-done:
		t.Fatalf("EventSub stopped before READY: %v", err)
	case <-time.After(30 * time.Second):
		stopRun()
		<-done
		t.Fatal("EventSub did not become ready within 30 seconds")
	}
	seen := map[string]int{}
	remaining := len(f.sink.expected)
	deadline := time.NewTimer(120 * time.Second)
	defer deadline.Stop()
	for remaining > 0 {
		select {
		case command := <-f.sink.got:
			seen[command]++
			remaining--
			t.Logf("accepted service action: %s", command)
		case err := <-f.sink.fail:
			stopRun()
			<-done
			t.Fatal(err)
		case err := <-retry:
			stopRun()
			<-done
			t.Fatal(err)
		case err := <-done:
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("EventSub stopped: %v", err)
			}
			if remaining > 0 {
				t.Fatalf("received command counts %v; expected five ordered service actions", seen)
			}
		case <-deadline.C:
			stopRun()
			<-done
			t.Fatalf("command window expired; received %v", seen)
		}
	}
	stopRun()
	<-done
	if calls := transport.calls.Load(); calls > 10 {
		t.Fatalf("live transport exceeded ten HTTP requests: %d", calls)
	}
	t.Log("transport accepted five ordered command service actions")
}

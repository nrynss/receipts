package connection

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
)

type resolverFunc func(context.Context, auth.ChannelContext) (Grant, error)

func (f resolverFunc) Resolve(ctx context.Context, a auth.ChannelContext) (Grant, error) {
	return f(ctx, a)
}

type tokenFixture struct {
	calls        atomic.Int32
	refresh      atomic.Int32
	accessErr    error
	refreshToken string
	lost         atomic.Int32
}

func (f *tokenFixture) AccessToken(context.Context, string) (string, error) {
	f.calls.Add(1)
	return "access-a", f.accessErr
}
func (f *tokenFixture) RefreshAfterUnauthorized(_ context.Context, _ string, rejected string) (string, error) {
	f.refresh.Add(1)
	if rejected != "access-a" {
		return "", auth.ErrReconnect
	}
	if f.refreshToken != "" {
		return f.refreshToken, nil
	}
	return "access-b", nil
}
func (f *tokenFixture) RevokeProviderAuthorizationIfCurrent(context.Context, string, string) error {
	f.lost.Add(1)
	return nil
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestStreamLiveBindsIdentityAndRefreshesOnce(t *testing.T) {
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "local-a", LiveSessionID: "live-a", Generation: 1}
	runtime := channelruntime.New(channelruntime.Config{MaxChannels: 2})
	if _, err := runtime.Start(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	tokens := &tokenFixture{}
	resolver := resolverFunc(func(_ context.Context, got auth.ChannelContext) (Grant, error) {
		if got.AccountID != "owner-a" {
			t.Fatal("wrong owner")
		}
		return Grant{ClientID: "client-a", SubjectID: "twitch-a", ChannelPlatformID: "twitch-a", Scopes: RequiredScopes()}, nil
	})
	transport := rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("user_id") != "twitch-a" {
			t.Errorf("unbound broadcaster: %q", r.URL.Query().Get("user_id"))
		}
		switch r.Header.Get("Authorization") {
		case "Bearer access-a":
			return response(401, `{"error":"expired"}`), nil
		case "Bearer access-b":
			return response(200, `{"data":[{"id":"stream-a"}]}`), nil
		default:
			t.Errorf("unexpected auth header")
			return response(500, ""), nil
		}
	})
	adapter := Adapter{Runtime: runtime, Resolver: resolver, Tokens: tokens, HTTP: &http.Client{Transport: transport}, BaseURL: "https://unit.invalid"}
	live, err := adapter.StreamLive(context.Background(), a)
	if err != nil || !live {
		t.Fatalf("StreamLive = %v, %v", live, err)
	}
	if tokens.refresh.Load() != 1 {
		t.Fatalf("refresh calls = %d", tokens.refresh.Load())
	}
	if tokens.calls.Load() != 1 {
		t.Fatalf("access token calls=%d, want one", tokens.calls.Load())
	}
}

func TestRepeatedUnauthorizedReportsReconnectWithoutTokenLeak(t *testing.T) {
	a := auth.ChannelContext{AccountID: "owner", ChannelID: "local", LiveSessionID: "live", Generation: 1}
	runtime := channelruntime.New(channelruntime.Config{})
	_, _ = runtime.Start(context.Background(), a)
	transport := rtFunc(func(*http.Request) (*http.Response, error) { return response(401, "secret-token-echo"), nil })
	tokens := &tokenFixture{refreshToken: "access-a"}
	adapter := Adapter{Runtime: runtime, Resolver: resolverFunc(func(context.Context, auth.ChannelContext) (Grant, error) {
		return Grant{ClientID: "client", SubjectID: "twitch", ChannelPlatformID: "twitch", Scopes: RequiredScopes()}, nil
	}), Tokens: tokens, HTTP: &http.Client{Transport: transport}, BaseURL: "https://unit.invalid"}
	_, err := adapter.StreamLive(context.Background(), a)
	if !errors.Is(err, ErrLost) || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("unexpected failure: %v", err)
	}
	if _, err := runtime.Resolve(a); err == nil {
		t.Fatal("authorization loss did not stop the channel runtime")
	}
	if tokens.refresh.Load() != 1 || tokens.lost.Load() != 1 {
		t.Fatalf("refresh/loss calls=%d/%d", tokens.refresh.Load(), tokens.lost.Load())
	}
}

func TestIdentityMismatchAndTransientErrorsDoNotRefresh(t *testing.T) {
	for _, tc := range []struct {
		name        string
		grant       Grant
		status      int
		wantBinding bool
	}{
		{name: "identity mismatch", grant: Grant{ClientID: "client", SubjectID: "foreign", ChannelPlatformID: "twitch", Scopes: RequiredScopes()}, status: 200, wantBinding: true},
		{name: "rate limited", grant: Grant{ClientID: "client", SubjectID: "twitch", ChannelPlatformID: "twitch", Scopes: RequiredScopes()}, status: 429},
		{name: "transient", grant: Grant{ClientID: "client", SubjectID: "twitch", ChannelPlatformID: "twitch", Scopes: RequiredScopes()}, status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := auth.ChannelContext{AccountID: "owner", ChannelID: "local", LiveSessionID: "live", Generation: 1}
			runtime := channelruntime.New(channelruntime.Config{})
			_, _ = runtime.Start(context.Background(), a)
			tokens := &tokenFixture{}
			var calls atomic.Int32
			adapter := Adapter{Runtime: runtime, Resolver: resolverFunc(func(context.Context, auth.ChannelContext) (Grant, error) { return tc.grant, nil }), Tokens: tokens, HTTP: &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return response(tc.status, `{}`), nil })}, BaseURL: "https://unit.invalid"}
			_, err := adapter.StreamLive(context.Background(), a)
			if tc.wantBinding {
				if !errors.Is(err, ErrBinding) {
					t.Fatalf("expected binding rejection, got %v", err)
				}
				if calls.Load() != 0 {
					t.Fatal("foreign identity reached Helix")
				}
				return
			}
			if err == nil {
				t.Fatal("expected provider failure")
			}
			if calls.Load() != 1 {
				t.Fatalf("request count = %d, want bounded single request", calls.Load())
			}
			if tokens.refresh.Load() != 0 {
				t.Fatalf("refresh count = %d", tokens.refresh.Load())
			}
		})
	}
}

func TestStoppingOneChannelLeavesOtherActive(t *testing.T) {
	manager := channelruntime.New(channelruntime.Config{MaxChannels: 2})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "local-a", LiveSessionID: "live-a", Generation: 1}
	b := auth.ChannelContext{AccountID: "owner-b", ChannelID: "local-b", LiveSessionID: "live-b", Generation: 1}
	if _, err := manager.Start(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(a); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Resolve(b); err != nil {
		t.Fatalf("channel B stopped with A: %v", err)
	}
}

func TestUnauthorizedFromRegisteredWorkerCancelsWithoutSelfDeadlock(t *testing.T) {
	a := auth.ChannelContext{AccountID: "owner", ChannelID: "local", LiveSessionID: "live", Generation: 1}
	manager := channelruntime.New(channelruntime.Config{})
	run, err := manager.Start(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	tokens := &tokenFixture{refreshToken: "access-a"}
	adapter := Adapter{Runtime: manager, Resolver: resolverFunc(func(context.Context, auth.ChannelContext) (Grant, error) {
		return Grant{ClientID: "client", SubjectID: "twitch", ChannelPlatformID: "twitch", Scopes: RequiredScopes()}, nil
	}), Tokens: tokens, HTTP: &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) { return response(401, "unauthorized"), nil })}, BaseURL: "https://unit.invalid"}
	done := make(chan struct{})
	if !run.Go(func(ctx context.Context) { defer close(done); _, _ = adapter.StreamLive(ctx, a) }) {
		t.Fatal("worker did not start")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker deadlocked while handling authorization loss")
	}
	if !errors.Is(run.Context.Err(), context.Canceled) {
		t.Fatalf("runtime cancellation=%v", run.Context.Err())
	}
	if tokens.lost.Load() != 1 {
		t.Fatalf("loss handler calls=%d", tokens.lost.Load())
	}
}

func TestStaleAuthorizationLossCannotRevokeReplacementRuntime(t *testing.T) {
	manager := channelruntime.New(channelruntime.Config{})
	old := auth.ChannelContext{AccountID: "owner", ChannelID: "local", LiveSessionID: "old-live", Generation: 1}
	if _, err := manager.Start(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(old); err != nil {
		t.Fatal(err)
	}
	current := auth.ChannelContext{AccountID: "owner", ChannelID: "local", LiveSessionID: "new-live", Generation: 2}
	if _, err := manager.Start(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	tokens := &tokenFixture{}
	adapter := Adapter{Runtime: manager, Tokens: tokens}
	_ = adapter.authorizationLost(context.Background(), old, "old-token", ErrLost)
	if tokens.lost.Load() != 0 {
		t.Fatal("stale response invoked authorization-loss callback")
	}
	if _, err := manager.Resolve(current); err != nil {
		t.Fatalf("stale response canceled replacement: %v", err)
	}
}

func TestProbeStreamStatusWorksWithoutRuntimeAndUsesObservedIdentity(t *testing.T) {
	tokens := &tokenFixture{}
	var calls atomic.Int32
	adapter := Adapter{Resolver: resolverFunc(func(context.Context, auth.ChannelContext) (Grant, error) {
		return Grant{ClientID: "client", SubjectID: "twitch-id", ChannelPlatformID: "twitch-id"}, nil
	}), Tokens: tokens, HTTP: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Query().Get("user_id") != "twitch-id" {
			t.Errorf("query user_id=%q", r.URL.Query().Get("user_id"))
		}
		return response(200, `{"data":[{"id":"stream"}]}`), nil
	})}, BaseURL: "https://unit.invalid"}
	status, err := adapter.ProbeStreamStatus(context.Background(), auth.ChannelContext{AccountID: "owner", ChannelID: "local-channel"})
	if err != nil || !status.Known || !status.Live || status.ObservedAt.IsZero() {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	if calls.Load() != 1 || tokens.calls.Load() != 1 {
		t.Fatalf("Helix/token calls=%d/%d", calls.Load(), tokens.calls.Load())
	}
}

func TestGetClipsBindingNeedsNoAdditionalScope(t *testing.T) {
	if got := scopeFor(http.MethodGet, "/clips"); got != "-" {
		t.Fatalf("GET /clips scope=%q, want no extra scope", got)
	}
}

func TestRuntimeCancellationReachesMetadataLookup(t *testing.T) {
	a := auth.ChannelContext{AccountID: "owner", ChannelID: "local", LiveSessionID: "live", Generation: 1}
	manager := channelruntime.New(channelruntime.Config{})
	run, _ := manager.Start(context.Background(), a)
	started := make(chan struct{})
	finished := make(chan error, 1)
	adapter := Adapter{Runtime: manager, Tokens: &tokenFixture{}, Resolver: resolverFunc(func(ctx context.Context, _ auth.ChannelContext) (Grant, error) {
		close(started)
		<-ctx.Done()
		return Grant{}, ctx.Err()
	})}
	go func() { _, err := adapter.StreamLive(context.Background(), a); finished <- err }()
	<-started
	if err := manager.Cancel(a); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lookup error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime cancellation did not cancel metadata lookup")
	}
	if run.Context.Err() == nil {
		t.Fatal("runtime context remained active")
	}
}

func TestRevokedGrantStopsOnlyItsRuntime(t *testing.T) {
	manager := channelruntime.New(channelruntime.Config{MaxChannels: 2})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "local-a", LiveSessionID: "live-a", Generation: 1}
	b := auth.ChannelContext{AccountID: "owner-b", ChannelID: "local-b", LiveSessionID: "live-b", Generation: 1}
	_, _ = manager.Start(context.Background(), a)
	_, _ = manager.Start(context.Background(), b)
	adapter := Adapter{Runtime: manager, Resolver: resolverFunc(func(context.Context, auth.ChannelContext) (Grant, error) {
		return Grant{ClientID: "client-a", SubjectID: "twitch-a", ChannelPlatformID: "twitch-a", Scopes: RequiredScopes()}, nil
	}), Tokens: &tokenFixture{accessErr: auth.ErrReconnect}}
	_, err := adapter.StreamLive(context.Background(), a)
	if !errors.Is(err, ErrLost) {
		t.Fatalf("revoked grant error=%v", err)
	}
	if _, err = manager.Resolve(a); err == nil {
		t.Fatal("revoked grant left its runtime active")
	}
	if _, err = manager.Resolve(b); err != nil {
		t.Fatalf("revoked grant stopped another channel: %v", err)
	}
}

package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	runtime "receipts/internal/runtime"
	"receipts/internal/store"
	"receipts/internal/twitch/connection"
)

type resolver struct{}

func (resolver) Resolve(context.Context, auth.ChannelContext) (connection.Grant, error) {
	return connection.Grant{ClientID: "client", SubjectID: "subject", ChannelPlatformID: "subject", Scopes: []string{"user:read:chat"}}, nil
}

type tokenSource struct{}

func (tokenSource) AccessToken(context.Context, string) (string, error) { return "opaque", nil }
func (tokenSource) RefreshAfterUnauthorized(context.Context, string, string) (string, error) {
	return "", nil
}

func TestConfirmedBroadcastActivatesOwnerSessionAndOfflineCancels(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "live.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(ctx, "subject", "streamer")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','streamer',?,X'00',?,'[]','connected',?)`, channel.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	var live atomic.Bool
	var unknown atomic.Bool
	live.Store(true)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/streams" {
			http.NotFound(w, r)
			return
		}
		if unknown.Load() {
			http.Error(w, "provider unavailable", http.StatusBadGateway)
			return
		}
		if live.Load() {
			_, _ = w.Write([]byte(`{"data":[{"id":"broadcast-a"}]}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[]}`))
		}
	}))
	defer provider.Close()
	manager := runtime.New(runtime.Config{MaxChannels: 1})
	adapter := &connection.Adapter{Runtime: manager, Resolver: resolver{}, Tokens: tokenSource{}, HTTP: provider.Client(), BaseURL: provider.URL}
	a := &Authority{Adapter: adapter, Ledger: ledger, Runtime: manager, Freshness: time.Minute, PollInterval: time.Hour, Root: context.Background()}
	requestCtx, cancelRequest := context.WithCancel(ctx)
	identity := auth.ChannelContext{AccountID: "owner", ChannelID: channel.ID}
	if err = a.StartWithMicrophone(requestCtx, identity, true); err != nil {
		t.Fatal(err)
	}
	cancelRequest()
	sessionID, broadcastID, _, mic, generation, err := ledger.CurrentLiveAuthority(ctx, "owner", channel.ID)
	if err != nil || broadcastID != "broadcast-a" || !mic {
		t.Fatalf("persisted authority session=%q broadcast=%q mic=%v err=%v", sessionID, broadcastID, mic, err)
	}
	if err = ledger.VerifyLiveSession(ctx, "owner", channel.ID, sessionID); err != nil {
		t.Fatalf("live authority ended with request: %v", err)
	}
	authority := auth.ChannelContext{AccountID: "owner", ChannelID: channel.ID, LiveSessionID: sessionID, Generation: 1}
	if _, err = manager.Resolve(authority); err != nil {
		t.Fatalf("request cancellation stopped runtime: %v", err)
	}
	unknown.Store(true)
	state, err := a.Status(ctx, identity)
	if state != "unknown" {
		t.Fatalf("unknown status state=%q err=%v", state, err)
	}
	if err = manager.WaitChannelStopped(ctx, "owner", channel.ID); err != nil {
		t.Fatal(err)
	}
	resumeID, resumeBroadcast, _, resumeMic, resumeGeneration, err := ledger.CurrentLiveAuthority(ctx, "owner", channel.ID)
	if err != nil || resumeID != sessionID || resumeBroadcast != "broadcast-a" || resumeMic || resumeGeneration != generation {
		t.Fatalf("unknown stop did not retain only ledger identity with mic revoked: id=%q broadcast=%q mic=%v err=%v", resumeID, resumeBroadcast, resumeMic, err)
	}
	unknown.Store(false)
	if err = a.Start(ctx, identity); err != nil {
		t.Fatalf("fresh same-broadcast reactivation: %v", err)
	}
	if _, err = manager.Resolve(authority); err == nil {
		t.Fatal("same-broadcast reactivation reused the old runtime generation")
	}
	authority.Generation = 2
	if _, err = manager.Resolve(authority); err != nil {
		t.Fatalf("new runtime generation missing: %v", err)
	}
	state, err = a.Status(ctx, identity)
	if err != nil || state != "session_active" || a.AudioCaptureActive() {
		t.Fatalf("runtime/audio distinction state=%q capture=%v err=%v", state, a.AudioCaptureActive(), err)
	}
	live.Store(false)
	state, err = a.Status(ctx, identity)
	if err != nil || state != "offline" {
		t.Fatalf("offline state=%q err=%v", state, err)
	}
	deadline := time.Now().Add(time.Second)
	stoppedOffline := false
	for time.Now().Before(deadline) {
		if _, resolveErr := manager.Resolve(authority); resolveErr != nil {
			if _, _, _, mic, _, currentErr := ledger.CurrentLiveAuthority(ctx, "owner", channel.ID); currentErr == nil || mic {
				t.Fatal("offline exit retained the live session or microphone permission")
			}
			stoppedOffline = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !stoppedOffline {
		t.Fatal("offline status left runtime active")
	}
	if err = manager.WaitChannelStopped(ctx, "owner", channel.ID); err != nil {
		t.Fatal(err)
	}
	live.Store(true)
	var detached atomic.Bool
	a.Attach = func(context.Context, auth.ChannelContext) (func(), error) {
		return func() { detached.Store(true) }, errors.New("worker attach failed")
	}
	if err = a.Start(ctx, identity); err == nil {
		t.Fatal("failed worker attach reported a successful activation")
	}
	if err = manager.WaitChannelStopped(ctx, "owner", channel.ID); err != nil {
		t.Fatal(err)
	}
	if !detached.Load() {
		t.Fatal("attach failure leaked its cleanup callback")
	}
	if _, _, checked, mic, _, err := ledger.CurrentLiveAuthority(ctx, "owner", channel.ID); err != nil || checked.UnixMilli() != 0 || mic {
		t.Fatalf("failed worker attach left active freshness or mic permission: checked=%v mic=%v err=%v", checked, mic, err)
	}
}

func TestStopDuringStatusProbeCancelsPendingActivation(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "stop-race.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(ctx, "subject", "streamer")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','streamer',?,X'00',?,'[]','connected',?)`, channel.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte(`{"data":[{"id":"broadcast-a"}]}`))
	}))
	defer provider.Close()
	manager := runtime.New(runtime.Config{MaxChannels: 1})
	a := &Authority{Adapter: &connection.Adapter{Runtime: manager, Resolver: resolver{}, Tokens: tokenSource{}, HTTP: provider.Client(), BaseURL: provider.URL}, Ledger: ledger, Runtime: manager, Freshness: time.Minute}
	identity := auth.ChannelContext{AccountID: "owner", ChannelID: channel.ID}
	started := make(chan error, 1)
	go func() { started <- a.Start(ctx, identity) }()
	<-entered
	stopped := make(chan error, 1)
	go func() { stopped <- a.Stop(ctx, identity) }()
	intentDeadline := time.Now().Add(time.Second)
	for !a.activationCurrent(identity.ChannelID, 2) && time.Now().Before(intentDeadline) {
		time.Sleep(time.Millisecond)
	}
	if !a.activationCurrent(identity.ChannelID, 2) {
		t.Fatal("stop did not record its activation intent")
	}
	close(release)
	if err = <-started; err == nil {
		t.Fatal("pending activation succeeded after stop intent")
	}
	if err = <-stopped; err != nil {
		t.Fatal(err)
	}
	if manager.Count() != 0 {
		t.Fatalf("active runtime count=%d", manager.Count())
	}
	if _, _, _, _, _, err = ledger.CurrentLiveAuthority(ctx, "owner", channel.ID); err == nil {
		t.Fatal("stop race persisted a live authority")
	}
}

func TestStopCancelsBlockingAttachmentBeforeWaitingForActivationLock(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "attach-stop.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(ctx, "subject", "streamer")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','streamer',?,X'00',?,'[]','connected',?)`, channel.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"broadcast-a"}]}`))
	}))
	defer provider.Close()
	manager := runtime.New(runtime.Config{MaxChannels: 1})
	a := &Authority{Adapter: &connection.Adapter{Runtime: manager, Resolver: resolver{}, Tokens: tokenSource{}, HTTP: provider.Client(), BaseURL: provider.URL}, Ledger: ledger, Runtime: manager, Freshness: time.Minute}
	entered := make(chan struct{})
	a.Attach = func(attachCtx context.Context, _ auth.ChannelContext) (func(), error) {
		close(entered)
		<-attachCtx.Done()
		return nil, nil
	}
	identity := auth.ChannelContext{AccountID: "owner", ChannelID: channel.ID}
	started := make(chan error, 1)
	go func() { started <- a.Start(ctx, identity) }()
	<-entered
	stopped := make(chan error, 1)
	go func() { stopped <- a.Stop(ctx, identity) }()
	select {
	case err = <-started:
		if err == nil {
			t.Fatal("activation succeeded after stop")
		}
	case <-time.After(time.Second):
		t.Fatal("stop failed to cancel the blocking attachment")
	}
	select {
	case err = <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stop remained blocked after attachment cancellation")
	}
	if manager.Count() != 0 {
		t.Fatalf("active runtime count=%d", manager.Count())
	}
}

func TestFreshnessWatchdogCancelsHungProbeAndRejectsLateConfirmation(t *testing.T) {
	if (&Authority{}).freshness() != 30*time.Second {
		t.Fatal("disabled freshness did not use a finite default")
	}
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "freshness.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(ctx, "subject", "streamer")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','streamer',?,X'00',?,'[]','connected',?)`, channel.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	probeStarted, release := make(chan struct{}), make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 1 {
			close(probeStarted)
			<-release
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"broadcast-a"}]}`))
	}))
	defer provider.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	manager := runtime.New(runtime.Config{MaxChannels: 1})
	a := &Authority{Adapter: &connection.Adapter{Runtime: manager, Resolver: resolver{}, Tokens: tokenSource{}, HTTP: provider.Client(), BaseURL: provider.URL}, Ledger: ledger, Runtime: manager, Freshness: 40 * time.Millisecond, PollInterval: 10 * time.Millisecond}
	identity := auth.ChannelContext{AccountID: "owner", ChannelID: channel.ID}
	if err = a.Start(ctx, identity); err != nil {
		t.Fatal(err)
	}
	<-probeStarted
	if err = manager.WaitChannelStopped(ctx, "owner", channel.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	sessionID, broadcastID, checkedAt, mic, generation, err := ledger.CurrentLiveAuthority(ctx, "owner", channel.ID)
	if err != nil || sessionID == "" || broadcastID != "broadcast-a" || checkedAt.UnixMilli() != 0 || mic || generation != 1 {
		t.Fatalf("expired authority refreshed by late probe: id=%q broadcast=%q checked=%v mic=%v generation=%d err=%v", sessionID, broadcastID, checkedAt, mic, generation, err)
	}
}

func TestCapacityRejectionPreservesExistingBroadcastAndSessionGeneration(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "capacity.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(ctx, "subject", "streamer")
	if err != nil {
		t.Fatal(err)
	}
	other, err := ledger.CreateChannel(ctx, "other-subject", "other")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, owner := range []struct{ id, subject, login, channel string }{{"owner", "subject", "streamer", channel.ID}, {"other-owner", "other-subject", "other", other.ID}} {
		if _, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES(?,'twitch',?,?,?,X'00',?,'[]','connected',?)`, owner.id, owner.subject, owner.login, owner.channel, now.Add(time.Hour).UnixMilli(), now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	prior, err := ledger.StartOwnedLiveSession(ctx, "owner", channel.ID, "broadcast-a", now, false)
	if err != nil {
		t.Fatal(err)
	}
	manager := runtime.New(runtime.Config{MaxChannels: 1})
	otherAuthority := auth.ChannelContext{AccountID: "other-owner", ChannelID: other.ID, LiveSessionID: "other-live", Generation: 1}
	if _, err = manager.Start(ctx, otherAuthority); err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"broadcast-a"}]}`))
	}))
	defer provider.Close()
	a := &Authority{Adapter: &connection.Adapter{Runtime: manager, Resolver: resolver{}, Tokens: tokenSource{}, HTTP: provider.Client(), BaseURL: provider.URL}, Ledger: ledger, Runtime: manager, Freshness: time.Minute}
	if err = a.Start(ctx, auth.ChannelContext{AccountID: "owner", ChannelID: channel.ID}); !errors.Is(err, runtime.ErrCapacity) {
		t.Fatalf("capacity start error=%v", err)
	}
	id, broadcast, checked, mic, generation, err := ledger.CurrentLiveAuthority(ctx, "owner", channel.ID)
	if err != nil || id != prior.Session.ID || broadcast != "broadcast-a" || checked.UnixMilli() != now.UnixMilli() || mic || generation != prior.Generation {
		t.Fatalf("capacity rejection overwrote session authority: id=%q broadcast=%q checked=%v mic=%v generation=%d err=%v", id, broadcast, checked, mic, generation, err)
	}
	if _, err = manager.Resolve(otherAuthority); err != nil {
		t.Fatalf("capacity rejection affected the other owner: %v", err)
	}
}

func TestUnknownProbeDoesNotCreateAuthority(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "unknown.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(ctx, "subject", "streamer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','streamer',?,X'00',?,'[]','connected',?)`, channel.ID, time.Now().Add(time.Hour).UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "provider unavailable", http.StatusBadGateway)
	}))
	defer provider.Close()
	manager := runtime.New(runtime.Config{MaxChannels: 1})
	adapter := &connection.Adapter{Runtime: manager, Resolver: resolver{}, Tokens: tokenSource{}, HTTP: provider.Client(), BaseURL: provider.URL}
	a := &Authority{Adapter: adapter, Ledger: ledger, Runtime: manager, Freshness: time.Minute}
	if err = a.Start(ctx, auth.ChannelContext{AccountID: "owner", ChannelID: channel.ID}); err == nil {
		t.Fatal("unknown status activated authority")
	}
	if manager.Count() != 0 {
		t.Fatalf("active runtime count=%d", manager.Count())
	}
	if _, _, _, _, _, err = ledger.CurrentLiveAuthority(ctx, "owner", channel.ID); err == nil {
		t.Fatal("unknown status persisted authority")
	}
}

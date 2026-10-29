package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
)

func TestChannelsHaveIndependentAuthorityAndStop(t *testing.T) {
	manager := New(Config{MaxChannels: 2})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", LiveSessionID: "session-a", Generation: 1}
	b := auth.ChannelContext{AccountID: "owner-b", ChannelID: "channel-b", LiveSessionID: "session-b", Generation: 1}
	one, err := manager.Start(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	two, err := manager.Start(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if one.Events == two.Events || one.Epoch == two.Epoch {
		t.Fatal("channels shared publisher or epoch")
	}
	if _, err := manager.Resolve(auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-b", LiveSessionID: "session-b", Generation: 1}); !errors.Is(err, ErrStale) {
		t.Fatalf("guessed channel authority resolved: %v", err)
	}
	if err := manager.Stop(a); err != nil {
		t.Fatal(err)
	}
	if one.Context.Err() == nil {
		t.Fatal("stopped channel context remains active")
	}
	if _, err := manager.Resolve(b); err != nil {
		t.Fatalf("stopping A affected B: %v", err)
	}
}

func TestStopWaitsForWorkersBeforeReusingCapacity(t *testing.T) {
	manager := New(Config{MaxChannels: 1})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", LiveSessionID: "live-a", Generation: 1}
	b := auth.ChannelContext{AccountID: "owner-b", ChannelID: "channel-b", LiveSessionID: "live-b", Generation: 1}
	channel, err := manager.Start(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	started, finish := make(chan struct{}), make(chan struct{})
	if !channel.Go(func(ctx context.Context) { close(started); <-finish }) {
		t.Fatal("worker refused")
	}
	<-started
	stopped := make(chan error, 1)
	go func() { stopped <- manager.Stop(a) }()
	select {
	case <-channel.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel session")
	}
	if _, err := manager.Start(context.Background(), b); !errors.Is(err, ErrCapacity) {
		t.Fatalf("slot freed before worker returned: %v", err)
	}
	close(finish)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(context.Background(), b); err != nil {
		t.Fatalf("slot not reaped: %v", err)
	}
}

func TestParentCancellationReapsChannelAndRejectsDelayedWork(t *testing.T) {
	manager := New(Config{MaxChannels: 1})
	parent, cancel := context.WithCancel(context.Background())
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", LiveSessionID: "live-a", Generation: 3}
	channel, err := manager.Start(parent, a)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-channel.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not stop runtime")
	}
	deadline := time.Now().Add(time.Second)
	for manager.Count() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if manager.Count() != 0 {
		t.Fatal("parent cancellation did not reap channel")
	}
	if channel.Go(func(context.Context) {}) {
		t.Fatal("late work admitted")
	}
}

type sessionVerifierError struct{ err error }

func (v sessionVerifierError) VerifyLiveSession(context.Context, string, string, string) error {
	return v.err
}

type sessionVerifierSequence struct {
	calls   int
	second  error
	manager *Manager
}

func (v *sessionVerifierSequence) VerifyLiveSession(context.Context, string, string, string) error {
	v.calls++
	if v.calls == 2 && v.manager.Count() != 1 {
		return errors.New("runtime was not registered before second verification")
	}
	if v.calls == 2 {
		return v.second
	}
	return nil
}

func TestStartVerifiedRequiresPersistedLiveSession(t *testing.T) {
	manager := New(Config{MaxChannels: 1})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", LiveSessionID: "live-a", Generation: 1}
	if _, err := manager.StartVerified(context.Background(), a, sessionVerifierError{ErrStale}); !errors.Is(err, ErrSessionNotLive) {
		t.Fatalf("offline start error=%v", err)
	}
	if manager.Count() != 0 {
		t.Fatal("rejected session consumed capacity")
	}
	if _, err := manager.StartVerified(context.Background(), a, sessionVerifierError{}); err != nil {
		t.Fatal(err)
	}
}

func TestStartVerifiedRechecksSessionAfterRegistration(t *testing.T) {
	manager := New(Config{MaxChannels: 1})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", LiveSessionID: "live-a", Generation: 1}
	revoked := errors.New("session revoked between checks")
	verifier := &sessionVerifierSequence{second: revoked, manager: manager}
	channel, err := manager.StartVerified(context.Background(), a, verifier)
	if channel != nil || !errors.Is(err, ErrSessionNotLive) || !errors.Is(err, revoked) {
		t.Fatalf("StartVerified channel=%v err=%v", channel, err)
	}
	if manager.Count() != 0 {
		t.Fatalf("revoked runtime survived activation: %d", manager.Count())
	}
}

func TestPauseStateIsPerChannel(t *testing.T) {
	manager := New(Config{MaxChannels: 2})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", LiveSessionID: "live-a", Generation: 1}
	b := auth.ChannelContext{AccountID: "owner-b", ChannelID: "channel-b", LiveSessionID: "live-b", Generation: 1}
	ca, err := manager.Start(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := manager.Start(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetPaused(a, true); err != nil {
		t.Fatal(err)
	}
	if !ca.Paused() || cb.Paused() {
		t.Fatal("pause state crossed channel boundary")
	}
}

func TestSpendRequiresCurrentChannelGenerationAndStopCancelsPaidWork(t *testing.T) {
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: filepath.Join(t.TempDir(), "runtime-spend.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	budgets, err := OpenBudgets(context.Background(), db, BudgetConfig{GlobalLimit: 100, ChannelLimit: 80})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(Config{MaxChannels: 2})
	manager.SetBudgets(budgets)
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", LiveSessionID: "live-a", Generation: 1}
	b := auth.ChannelContext{AccountID: "owner-b", ChannelID: "channel-b", LiveSessionID: "live-b", Generation: 1}
	if _, err = manager.Start(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Start(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if err := manager.Spend(cancelled, a, "judge", "cancelled-before-start", 1, func(context.Context) (cost.Usage, error) {
		called = true
		return cost.Usage{Price: 1, Measured: true}, nil
	}); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("cancelled caller work called=%v err=%v", called, err)
	}
	called = false
	if err := manager.Spend(context.Background(), auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-b", LiveSessionID: "live-b", Generation: 1}, "judge", "guessed", 10, func(context.Context) (cost.Usage, error) {
		called = true
		return cost.Usage{Price: 1, Measured: true}, nil
	}); !errors.Is(err, ErrStale) || called {
		t.Fatalf("guessed channel work called=%v err=%v", called, err)
	}
	started := make(chan struct{})
	workDone := make(chan struct{})
	spendDone := make(chan error, 1)
	go func() {
		spendDone <- manager.Spend(context.Background(), a, "judge", "job-a", 10, func(ctx context.Context) (cost.Usage, error) {
			close(started)
			<-ctx.Done()
			close(workDone)
			return cost.Usage{}, ctx.Err()
		})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("paid work did not start")
	}
	stopDone := make(chan error, 1)
	go func() { stopDone <- manager.Stop(a) }()
	select {
	case <-workDone:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel paid work")
	}
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if err := <-spendDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("paid cancellation=%v", err)
	}
	if _, err := manager.Resolve(b); err != nil {
		t.Fatalf("stopping A affected B: %v", err)
	}
	called = false
	if err := manager.Spend(context.Background(), a, "judge", "late", 1, func(context.Context) (cost.Usage, error) {
		called = true
		return cost.Usage{Price: 1, Measured: true}, nil
	}); !errors.Is(err, ErrStale) || called {
		t.Fatalf("delayed job called=%v err=%v", called, err)
	}
}

func TestProviderRevocationCancelsOnlyItsAccount(t *testing.T) {
	manager := New(Config{MaxChannels: 2})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", LiveSessionID: "live-a", Generation: 1}
	b := auth.ChannelContext{AccountID: "owner-b", ChannelID: "channel-b", LiveSessionID: "live-b", Generation: 1}
	ca, err := manager.Start(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Start(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	manager.RevokeAccount(a.AccountID)
	select {
	case <-ca.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("account revoke did not cancel runtime")
	}
	deadline := time.Now().Add(time.Second)
	for manager.Count() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if manager.Count() != 1 {
		t.Fatalf("revoked runtime not reaped: active=%d", manager.Count())
	}
	if _, err := manager.Resolve(b); err != nil {
		t.Fatalf("revocation affected another account: %v", err)
	}
}

func TestCapacityAndStaleStop(t *testing.T) {
	manager := New(Config{MaxChannels: 1})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", LiveSessionID: "session-a", Generation: 1}
	b := auth.ChannelContext{AccountID: "owner-b", ChannelID: "channel-b", LiveSessionID: "session-b", Generation: 1}
	if _, err := manager.Start(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(context.Background(), b); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity error=%v", err)
	}
	if err := manager.Stop(b); !errors.Is(err, ErrStale) {
		t.Fatalf("cross-channel stop error=%v", err)
	}
}

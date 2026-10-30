// Package session owns confirmed live-session authority for hosted channels.
package session

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"receipts/internal/api"
	"receipts/internal/auth"
	runtime "receipts/internal/runtime"
	"receipts/internal/store"
	"receipts/internal/twitch/connection"
)

// Authority activates runtime lifecycle only; it does not imply audio capture.
type Authority struct {
	Adapter      *connection.Adapter
	Ledger       *store.Store
	Runtime      *runtime.Manager
	Freshness    time.Duration
	PollInterval time.Duration
	Root         context.Context
	Attach       func(context.Context, auth.ChannelContext) (func(), error)
	AttachAudio  func(context.Context, auth.ChannelContext, bool) (func(), error)
	AudioActive  func(auth.ChannelContext) bool
	mu           sync.Mutex
	locks        map[string]*sync.Mutex
	activations  map[string]uint64
	active       map[string]runtimeActivation
}

type runtimeActivation struct {
	intent    uint64
	authority auth.ChannelContext
}

func (a *Authority) CanStart() bool {
	return a != nil && a.Adapter != nil && a.Ledger != nil && a.Runtime != nil
}
func (a *Authority) AudioCaptureActive() bool { return false }
func (a *Authority) AudioCaptureFor(identity auth.ChannelContext) bool {
	return a.AudioActive != nil && a.AudioActive(identity)
}

func (a *Authority) freshness() time.Duration {
	if a.Freshness > 0 {
		return a.Freshness
	}
	return 30 * time.Second
}

func (a *Authority) nextActivation(channelID string) uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activations == nil {
		a.activations = make(map[string]uint64)
	}
	a.activations[channelID]++
	return a.activations[channelID]
}

func (a *Authority) activationCurrent(channelID string, expected uint64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.activations[channelID] == expected
}

func (a *Authority) registerRuntime(channelID string, intent uint64, authority auth.ChannelContext) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activations[channelID] != intent {
		return false
	}
	if a.active == nil {
		a.active = make(map[string]runtimeActivation)
	}
	a.active[channelID] = runtimeActivation{intent: intent, authority: authority}
	return true
}

func (a *Authority) clearRuntime(authority auth.ChannelContext) {
	a.mu.Lock()
	defer a.mu.Unlock()
	current, ok := a.active[authority.ChannelID]
	if ok && current.authority.AccountID == authority.AccountID && current.authority.LiveSessionID == authority.LiveSessionID && current.authority.Generation == authority.Generation {
		delete(a.active, authority.ChannelID)
	}
}

func (a *Authority) Status(ctx context.Context, identity auth.ChannelContext) (api.ListeningState, error) {
	if !a.CanStart() {
		return api.StateUnavailable, nil
	}
	if _, channel, release, err := a.Runtime.BindRequest(ctx, identity); err == nil {
		release()
		sessionID, broadcastID, checkedAt, _, generation, persistedErr := a.Ledger.CurrentLiveAuthority(ctx, identity.AccountID, identity.ChannelID)
		if persistedErr != nil || sessionID != channel.Authority.LiveSessionID || generation != channel.Authority.Generation || broadcastID == "" || time.Since(checkedAt) > a.freshness() {
			_ = a.Runtime.Cancel(channel.Authority)
			return api.StateUnknown, persistedErr
		}
		status, probeErr := a.Adapter.ProbeStreamStatus(ctx, channel.Authority)
		if _, currentErr := a.Runtime.Resolve(channel.Authority); currentErr != nil {
			return api.StateUnknown, nil
		}
		if isMissingGrant(probeErr) {
			_ = a.Runtime.Cancel(channel.Authority)
			return api.StateMissingGrant, nil
		}
		if probeErr != nil || !status.Known {
			_ = a.Runtime.Cancel(channel.Authority)
			return api.StateUnknown, probeErr
		}
		if time.Since(status.ObservedAt) > a.freshness() {
			_ = a.Runtime.Cancel(channel.Authority)
			return api.StateUnknown, nil
		}
		if !status.Live || status.BroadcastID != broadcastID {
			_ = a.Runtime.Cancel(channel.Authority)
			_ = a.Ledger.EndSessionGeneration(ctx, channel.Authority.ChannelID, channel.Authority.LiveSessionID, channel.Authority.Generation)
			return api.StateOffline, nil
		}
		return api.StateSessionActive, nil
	}
	status, err := a.Adapter.ProbeStreamStatus(ctx, identity)
	if isMissingGrant(err) {
		return api.StateMissingGrant, nil
	}
	if err != nil {
		return api.StateUnknown, err
	}
	if !status.Known {
		return api.StateUnknown, nil
	}
	if time.Since(status.ObservedAt) > a.freshness() {
		return api.StateUnknown, nil
	}
	if !status.Live {
		return api.StateOffline, nil
	}
	if !a.Runtime.HasCapacity() {
		return api.StateCapacity, nil
	}
	return api.StateNotListening, nil
}

func isMissingGrant(err error) bool {
	return errors.Is(err, auth.ErrReconnect) || errors.Is(err, auth.ErrUnauthorized) || errors.Is(err, connection.ErrLost)
}

func (a *Authority) Start(ctx context.Context, identity auth.ChannelContext) error {
	return a.StartWithOptions(ctx, identity, false, false)
}

// StartWithMicrophone records consent for this confirmed session only.
func (a *Authority) StartWithMicrophone(ctx context.Context, identity auth.ChannelContext, microphoneAllowed bool) error {
	return a.StartWithOptions(ctx, identity, microphoneAllowed, false)
}

// StartWithOptions requires a fresh explicit sole-voice attestation for this activation.
func (a *Authority) StartWithOptions(ctx context.Context, identity auth.ChannelContext, microphoneAllowed, soleBroadcaster bool) error {
	if !a.CanStart() || identity.Device || identity.AccountID == "" || identity.ChannelID == "" {
		return errors.New("session: owner authority unavailable")
	}
	unlock := a.channelLock(identity.ChannelID)
	defer unlock()
	activation := a.nextActivation(identity.ChannelID)
	probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	status, err := a.Adapter.ProbeStreamStatus(probeCtx, identity)
	if err != nil {
		return err
	}
	if !status.Known || !status.Live || status.BroadcastID == "" || time.Since(status.ObservedAt) > a.freshness() {
		return errors.New("session: confirmed live broadcast required")
	}
	if !a.activationCurrent(identity.ChannelID, activation) {
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, _, release, activeErr := a.Runtime.BindRequest(ctx, identity); activeErr == nil {
		release()
		return runtime.ErrActive
	}
	if !a.Runtime.HasCapacity() {
		return runtime.ErrCapacity
	}
	activationRecord, err := a.Ledger.StartOwnedLiveSession(ctx, identity.AccountID, identity.ChannelID, status.BroadcastID, status.ObservedAt, microphoneAllowed)
	if err != nil {
		return err
	}
	sess := activationRecord.Session
	authority := auth.ChannelContext{AccountID: identity.AccountID, ChannelID: identity.ChannelID, LiveSessionID: sess.ID, Generation: activationRecord.Generation}
	root := a.Root
	if root == nil {
		root = context.Background()
	}
	channel, err := a.Runtime.StartVerified(root, authority, a.Ledger)
	if err != nil {
		_ = a.Ledger.RollbackLiveActivation(context.Background(), activationRecord)
		return err
	}
	if !a.registerRuntime(identity.ChannelID, activation, authority) {
		_ = a.Runtime.Cancel(authority)
		_ = a.Ledger.ClearLivePermission(context.Background(), identity.ChannelID, sess.ID, authority.Generation)
		return context.Canceled
	}
	var lastConfirmed atomic.Int64
	lastConfirmed.Store(status.ObservedAt.UnixNano())
	if !channel.Go(func(runCtx context.Context) { a.monitor(runCtx, authority, status.BroadcastID, &lastConfirmed) }) || !channel.Go(func(runCtx context.Context) { a.watchdog(runCtx, authority, &lastConfirmed) }) {
		_ = a.Runtime.Cancel(authority)
		a.clearRuntime(authority)
		_ = a.Ledger.ClearLivePermission(context.Background(), identity.ChannelID, sess.ID, authority.Generation)
		return errors.New("session: runtime stopped during activation")
	}
	if a.Attach != nil || a.AttachAudio != nil {
		var detach func()
		var attachErr error
		if a.AttachAudio != nil {
			detach, attachErr = a.AttachAudio(channel.Context, authority, soleBroadcaster)
		} else {
			detach, attachErr = a.Attach(channel.Context, authority)
		}
		if attachErr != nil {
			if detach != nil {
				detach()
			}
			_ = a.Runtime.Cancel(authority)
			a.clearRuntime(authority)
			_ = a.Ledger.ClearLivePermission(context.Background(), identity.ChannelID, sess.ID, authority.Generation)
			return attachErr
		}
		if !a.activationCurrent(identity.ChannelID, activation) {
			if detach != nil {
				detach()
			}
			_ = a.Runtime.Cancel(authority)
			a.clearRuntime(authority)
			_ = a.Ledger.ClearLivePermission(context.Background(), identity.ChannelID, sess.ID, authority.Generation)
			return context.Canceled
		}
		if detach != nil && !channel.Go(func(runCtx context.Context) { <-runCtx.Done(); detach() }) {
			detach()
			_ = a.Runtime.Cancel(authority)
			a.clearRuntime(authority)
			_ = a.Ledger.ClearLivePermission(context.Background(), identity.ChannelID, sess.ID, authority.Generation)
			return errors.New("session: runtime stopped while attaching worker")
		}
	}
	if _, err := a.Runtime.Resolve(authority); err != nil {
		a.clearRuntime(authority)
		_ = a.Ledger.ClearLivePermission(context.Background(), identity.ChannelID, sess.ID, authority.Generation)
		return errors.New("session: authority expired during attachment")
	}
	return nil
}

func (a *Authority) monitor(ctx context.Context, authority auth.ChannelContext, broadcastID string, lastConfirmed *atomic.Int64) {
	defer func() {
		a.clearRuntime(authority)
		endCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = a.Ledger.ClearLivePermission(endCtx, authority.ChannelID, authority.LiveSessionID, authority.Generation)
		cancel()
	}()
	interval := a.PollInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
			status, err := a.Adapter.ProbeStreamStatus(probeCtx, authority)
			cancel()
			if err != nil || !status.Known || time.Since(status.ObservedAt) > a.freshness() {
				_ = a.Runtime.Cancel(authority)
				return
			}
			if !status.Live || status.BroadcastID != broadcastID {
				_ = a.Ledger.EndSessionGeneration(ctx, authority.ChannelID, authority.LiveSessionID, authority.Generation)
				_ = a.Runtime.Cancel(authority)
				return
			}
			if err = a.Ledger.TouchLiveAuthority(ctx, authority.AccountID, authority.ChannelID, authority.LiveSessionID, broadcastID, authority.Generation, status.ObservedAt); err != nil {
				_ = a.Runtime.Cancel(authority)
				return
			}
			lastConfirmed.Store(status.ObservedAt.UnixNano())
		}
	}
}

func (a *Authority) watchdog(ctx context.Context, authority auth.ChannelContext, lastConfirmed *atomic.Int64) {
	interval := a.freshness() / 4
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			last := time.Unix(0, lastConfirmed.Load())
			if time.Since(last) > a.freshness() {
				_ = a.Runtime.Cancel(authority)
				return
			}
		}
	}
}

func (a *Authority) channelLock(channelID string) func() {
	a.mu.Lock()
	if a.locks == nil {
		a.locks = make(map[string]*sync.Mutex)
	}
	lock := a.locks[channelID]
	if lock == nil {
		lock = &sync.Mutex{}
		a.locks[channelID] = lock
	}
	a.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (a *Authority) Stop(ctx context.Context, identity auth.ChannelContext) error {
	if !a.CanStart() || identity.Device || identity.AccountID == "" || identity.ChannelID == "" {
		return auth.ErrNoChannelContext
	}
	a.mu.Lock()
	if a.activations == nil {
		a.activations = make(map[string]uint64)
	}
	a.activations[identity.ChannelID]++
	activation := a.activations[identity.ChannelID]
	target, hasTarget := a.active[identity.ChannelID]
	if hasTarget && target.authority.AccountID == identity.AccountID && target.intent < activation {
		delete(a.active, identity.ChannelID)
	} else {
		hasTarget = false
	}
	a.mu.Unlock()
	if hasTarget {
		_ = a.Runtime.Cancel(target.authority)
	}
	unlock := a.channelLock(identity.ChannelID)
	defer unlock()
	if !a.activationCurrent(identity.ChannelID, activation) {
		return nil
	}
	if err := a.Runtime.WaitChannelStopped(ctx, identity.AccountID, identity.ChannelID); err != nil {
		return err
	}
	sessionID, _, _, _, generation, err := a.Ledger.CurrentLiveAuthority(ctx, identity.AccountID, identity.ChannelID)
	if err == nil {
		if hasTarget && (generation != target.authority.Generation || sessionID != target.authority.LiveSessionID) {
			return nil
		}
		return a.Ledger.EndSessionGeneration(ctx, identity.ChannelID, sessionID, generation)
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return err
}

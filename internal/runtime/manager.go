// Package runtime supervises one isolated live authority per channel.
package runtime

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/id"
	"receipts/internal/auth"
	"receipts/internal/events"
)

var (
	ErrCapacity       = errors.New("runtime: channel capacity exhausted")
	ErrActive         = errors.New("runtime: channel already active")
	ErrStale          = errors.New("runtime: session authority is stale")
	ErrSessionNotLive = errors.New("runtime: persisted live session required")
)

type LiveSessionVerifier interface {
	VerifyLiveSession(context.Context, string, string, string) error
}

type Config struct{ MaxChannels int }
type Channel struct {
	Authority auth.ChannelContext
	Epoch     string
	Events    *events.Publisher
	Context   context.Context
	cancel    context.CancelFunc
	workers   sync.WaitGroup
	stopping  atomic.Bool
	paused    atomic.Bool
	launchMu  sync.Mutex
}

// Manager owns per-channel cancellation and publisher state. A stop is scoped
// to one channel and invalidates its session authority before cancellation.
type Manager struct {
	mu      sync.RWMutex
	max     int
	active  map[string]*Channel
	budgets *Budgets
}

func (m *Manager) SetBudgets(budgets *Budgets) { m.mu.Lock(); m.budgets = budgets; m.mu.Unlock() }

func New(cfg Config) *Manager {
	if cfg.MaxChannels <= 0 {
		cfg.MaxChannels = 2
	}
	return &Manager{max: cfg.MaxChannels, active: make(map[string]*Channel)}
}

func (m *Manager) Start(parent context.Context, authority auth.ChannelContext) (*Channel, error) {
	if authority.AccountID == "" || authority.ChannelID == "" || authority.LiveSessionID == "" || authority.Generation == 0 {
		return nil, auth.ErrNoChannelContext
	}
	epoch, err := id.New()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[authority.ChannelID] != nil {
		return nil, ErrActive
	}
	if len(m.active) >= m.max {
		return nil, ErrCapacity
	}
	ctx, cancel := context.WithCancel(parent)
	channel := &Channel{Authority: authority, Epoch: epoch, Events: events.NewPublisher(epoch), Context: ctx, cancel: cancel}
	m.active[authority.ChannelID] = channel
	go func() {
		<-ctx.Done()
		channel.launchMu.Lock()
		channel.stopping.Store(true)
		channel.launchMu.Unlock()
		channel.workers.Wait()
		m.mu.Lock()
		if m.active[authority.ChannelID] == channel {
			delete(m.active, authority.ChannelID)
		}
		m.mu.Unlock()
	}()
	return channel, nil
}

// StartVerified is the production activation seam. A login, guessed channel,
// or client assertion cannot activate a runtime without a persisted
// owner-bound session in live state.
func (m *Manager) StartVerified(parent context.Context, authority auth.ChannelContext, verifier LiveSessionVerifier) (*Channel, error) {
	if verifier == nil || authority.AccountID == "" || authority.ChannelID == "" || authority.LiveSessionID == "" || authority.Generation == 0 {
		return nil, ErrSessionNotLive
	}
	if err := verifier.VerifyLiveSession(parent, authority.AccountID, authority.ChannelID, authority.LiveSessionID); err != nil {
		return nil, errors.Join(ErrSessionNotLive, err)
	}
	channel, err := m.Start(parent, authority)
	if err != nil {
		return nil, err
	}
	// Close the gap between the preflight check and map insertion. Revocation
	// before insertion has no runtime for its hook to cancel; the second read
	// catches it. Revocation after insertion sees the registered runtime.
	if err := verifier.VerifyLiveSession(parent, authority.AccountID, authority.ChannelID, authority.LiveSessionID); err != nil {
		_ = m.Stop(authority)
		return nil, errors.Join(ErrSessionNotLive, err)
	}
	return channel, nil
}

// Go ties delayed work to the live session and prevents the slot from being
// released until every registered worker has returned.
func (c *Channel) Go(work func(context.Context)) bool {
	if c == nil || work == nil {
		return false
	}
	c.launchMu.Lock()
	defer c.launchMu.Unlock()
	if c.Context.Err() != nil || c.stopping.Load() {
		return false
	}
	c.workers.Add(1)
	go func() { defer c.workers.Done(); work(c.Context) }()
	return true
}

func (m *Manager) Resolve(authority auth.ChannelContext) (*Channel, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	channel := m.active[authority.ChannelID]
	if channel == nil || channel.Authority.AccountID != authority.AccountID ||
		channel.Authority.LiveSessionID != authority.LiveSessionID ||
		channel.Authority.Generation != authority.Generation || channel.Context.Err() != nil {
		return nil, ErrStale
	}
	return channel, nil
}

func (m *Manager) SetPaused(authority auth.ChannelContext, paused bool) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	channel := m.active[authority.ChannelID]
	if channel == nil || channel.Context.Err() != nil || channel.Authority.AccountID != authority.AccountID ||
		channel.Authority.LiveSessionID != authority.LiveSessionID || channel.Authority.Generation != authority.Generation {
		return ErrStale
	}
	channel.paused.Store(paused)
	return nil
}

func (c *Channel) Paused() bool { return c == nil || c.paused.Load() }

// BindRequest enriches owner/device identity with the active live authority
// and ties reads, controls and SSE lifetime to both HTTP cancellation and
// runtime stop. The returned cancel must be deferred by the handler.
func (m *Manager) BindRequest(parent context.Context, identity auth.ChannelContext) (context.Context, *Channel, context.CancelFunc, error) {
	m.mu.RLock()
	channel := m.active[identity.ChannelID]
	if channel == nil || channel.Context.Err() != nil || channel.Authority.AccountID != identity.AccountID {
		m.mu.RUnlock()
		return nil, nil, nil, ErrStale
	}
	channel.launchMu.Lock()
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(channel.Context, cancel)
	bound := channel.Authority
	bound.BrowserSessionID = identity.BrowserSessionID
	bound.Device = identity.Device
	bound.DeviceID = identity.DeviceID
	ctx = auth.WithChannelContext(ctx, bound)
	channel.launchMu.Unlock()
	m.mu.RUnlock()
	return ctx, channel, func() { stop(); cancel() }, nil
}

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := auth.ChannelFromContext(r.Context())
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx, channel, cancel, err := m.BindRequest(r.Context(), identity)
		if err != nil {
			http.Error(w, "channel runtime unavailable", http.StatusServiceUnavailable)
			return
		}
		defer cancel()
		// The manager publisher is authoritative for this channel; the API
		// chooses it from the context-bound manager entry, never request input.
		ctx = context.WithValue(ctx, runtimeChannelKey{}, channel)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type runtimeChannelKey struct{}

func ChannelFromContext(ctx context.Context) (*Channel, bool) {
	channel, ok := ctx.Value(runtimeChannelKey{}).(*Channel)
	return channel, ok && channel != nil
}

// Spend serializes authority validation against Stop, then meters the paid
// execution through the durable Keel account bound to the validated channel.
func (m *Manager) Spend(ctx context.Context, authority auth.ChannelContext, kind, ref string, estimate cost.Price, work cost.Work) error {
	if work == nil {
		return errors.New("runtime: paid work is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.RLock()
	channel := m.active[authority.ChannelID]
	if channel == nil || channel.Context.Err() != nil ||
		channel.Authority.AccountID != authority.AccountID ||
		channel.Authority.LiveSessionID != authority.LiveSessionID ||
		channel.Authority.Generation != authority.Generation {
		m.mu.RUnlock()
		return ErrStale
	}
	if m.budgets == nil {
		m.mu.RUnlock()
		return errors.New("runtime: spend budget unavailable")
	}
	budgets := m.budgets
	channel.launchMu.Lock()
	if channel.stopping.Load() || channel.Context.Err() != nil {
		channel.launchMu.Unlock()
		m.mu.RUnlock()
		return ErrStale
	}
	channel.workers.Add(1)
	channel.launchMu.Unlock()
	m.mu.RUnlock()
	defer channel.workers.Done()
	workCtx, cancel := context.WithCancel(channel.Context)
	stop := context.AfterFunc(ctx, cancel)
	defer func() { stop(); cancel() }()
	if channel.Context.Err() != nil {
		return ErrStale
	}
	guardedWork := func(callCtx context.Context) (cost.Usage, error) {
		if err := callCtx.Err(); err != nil {
			return cost.Usage{}, err
		}
		if err := channel.Context.Err(); err != nil {
			return cost.Usage{}, err
		}
		return work(callCtx)
	}
	_, err := budgets.Call(auth.WithChannelContext(workCtx, channel.Authority), authority.ChannelID, authority.LiveSessionID, kind, ref, estimate, guardedWork)
	return err
}

func (m *Manager) Stop(authority auth.ChannelContext) error {
	m.mu.Lock()
	channel := m.active[authority.ChannelID]
	if channel == nil || channel.Authority.AccountID != authority.AccountID ||
		channel.Authority.LiveSessionID != authority.LiveSessionID ||
		channel.Authority.Generation != authority.Generation {
		m.mu.Unlock()
		return ErrStale
	}
	channel.launchMu.Lock()
	channel.stopping.Store(true)
	channel.launchMu.Unlock()
	channel.cancel()
	m.mu.Unlock()
	channel.workers.Wait()
	m.mu.Lock()
	if m.active[authority.ChannelID] == channel {
		delete(m.active, authority.ChannelID)
	}
	m.mu.Unlock()
	return nil
}

// RevokeAccount invalidates and cancels every active runtime for one account.
// It returns after cancellation; the slot stays occupied until the supervisor
// has reaped every worker.
func (m *Manager) RevokeAccount(accountID string) {
	m.mu.Lock()
	for _, channel := range m.active {
		if channel.Authority.AccountID != accountID {
			continue
		}
		channel.launchMu.Lock()
		channel.stopping.Store(true)
		channel.launchMu.Unlock()
		channel.cancel()
	}
	m.mu.Unlock()
}

func (m *Manager) Count() int { m.mu.RLock(); defer m.mu.RUnlock(); return len(m.active) }

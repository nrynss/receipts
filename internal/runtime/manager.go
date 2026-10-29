// Package runtime supervises one isolated live authority per channel.
package runtime

import (
	"context"
	"errors"
	"sync"

	"github.com/nrynss/keel/id"
	"receipts/internal/auth"
	"receipts/internal/events"
)

var (
	ErrCapacity = errors.New("runtime: channel capacity exhausted")
	ErrActive   = errors.New("runtime: channel already active")
	ErrStale    = errors.New("runtime: session authority is stale")
)

type Config struct{ MaxChannels int }
type Channel struct {
	Authority auth.ChannelContext
	Epoch     string
	Events    *events.Publisher
	Context   context.Context
	cancel    context.CancelFunc
}

// Manager owns per-channel cancellation and publisher state. A stop is scoped
// to one channel and invalidates its session authority before cancellation.
type Manager struct {
	mu     sync.RWMutex
	max    int
	active map[string]*Channel
}

func New(cfg Config) *Manager {
	if cfg.MaxChannels <= 0 {
		cfg.MaxChannels = 2
	}
	return &Manager{max: cfg.MaxChannels, active: make(map[string]*Channel)}
}

func (m *Manager) Start(parent context.Context, authority auth.ChannelContext) (*Channel, error) {
	if authority.AccountID == "" || authority.ChannelID == "" || authority.SessionID == "" {
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
	return channel, nil
}

func (m *Manager) Resolve(authority auth.ChannelContext) (*Channel, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	channel := m.active[authority.ChannelID]
	if channel == nil || channel.Authority.AccountID != authority.AccountID ||
		channel.Authority.SessionID != authority.SessionID || channel.Context.Err() != nil {
		return nil, ErrStale
	}
	return channel, nil
}

func (m *Manager) Stop(authority auth.ChannelContext) error {
	m.mu.Lock()
	channel := m.active[authority.ChannelID]
	if channel == nil || channel.Authority.AccountID != authority.AccountID ||
		channel.Authority.SessionID != authority.SessionID {
		m.mu.Unlock()
		return ErrStale
	}
	delete(m.active, authority.ChannelID)
	channel.cancel()
	m.mu.Unlock()
	return nil
}

func (m *Manager) Count() int { m.mu.RLock(); defer m.mu.RUnlock(); return len(m.active) }

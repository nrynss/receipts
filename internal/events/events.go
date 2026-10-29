// Package events defines the public overlay event stream. Public payloads
// deliberately omit raw transcript text, attribution and void receipt data.
package events

import (
	"errors"
	"sync"
	"time"

	"github.com/nrynss/keel/stream"
)

const (
	TopicOverlay = "overlay"
	Snapshot     = "snapshot"
	Stamp        = "stamp"
	Status       = "status"
	ReplayLimit  = 128
)

type PublicReceipt struct {
	ID         string    `json:"id"`
	Claim      string    `json:"claim"`
	ClipURL    string    `json:"clip_url,omitempty"`
	ClipStatus string    `json:"clip_status"`
	Verdict    string    `json:"verdict"`
	CreatedAt  time.Time `json:"created_at"`
}

type OverlaySnapshot struct {
	Epoch        string          `json:"epoch"`
	Channel      string          `json:"channel"`
	Live         bool            `json:"live"`
	Receipts     []PublicReceipt `json:"receipts"`
	CurrentStamp *PublicReceipt  `json:"current_stamp,omitempty"`
	Sequence     uint64          `json:"sequence"`
}

type OverlayEvent struct {
	Epoch      string         `json:"epoch"`
	Sequence   uint64         `json:"sequence"`
	OccurredAt time.Time      `json:"occurred_at"`
	Receipt    *PublicReceipt `json:"receipt,omitempty"`
	Live       *bool          `json:"live,omitempty"`
}

type ReviewItem struct {
	ID        string    `json:"id"`
	Claim     string    `json:"claim"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
type Health struct {
	Status        string     `json:"status"`
	Mode          string     `json:"mode"`
	LiveConfirmed bool       `json:"live_confirmed"`
	LastEventAt   *time.Time `json:"last_event_at,omitempty"`
}

// Publisher uses Keel's bounded in-memory replay broker. Epoch distinguishes
// process restarts; sequence detects dropped events. Clients rebase from the
// authoritative snapshot endpoint on epoch changes or sequence gaps.
type Publisher struct {
	Broker *stream.Broker
	Epoch  string
	mu     sync.Mutex
	next   uint64
}

func NewPublisher(epoch string) *Publisher {
	return &Publisher{Broker: stream.New(stream.Config{Replay: ReplayLimit}), Epoch: epoch}
}

// Commit serializes one application state update with its public event. Every
// overlay-visible write must run through Commit so Snapshot cannot observe a
// changed read model with an older sequence (or vice versa).
func (p *Publisher) Commit(name string, update func() OverlayEvent) {
	if p == nil || p.Broker == nil || update == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.publishLocked(name, update())
}

func (p *Publisher) publishLocked(name string, payload OverlayEvent) {
	payload = cloneEvent(payload)
	payload.Epoch = p.Epoch
	p.next++
	payload.Sequence = p.next
	p.Broker.Publish(TopicOverlay, stream.Event{Name: name, Data: payload})
}

// Snapshot couples the read model with the latest event version. Callers
// must mutate overlay-visible state only inside Commit.
func (p *Publisher) Snapshot(read func() (OverlaySnapshot, error)) (OverlaySnapshot, error) {
	if p == nil || p.Broker == nil || read == nil {
		return OverlaySnapshot{}, errors.New("event publisher unavailable")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	value, err := read()
	if err != nil {
		return OverlaySnapshot{}, err
	}
	value = cloneSnapshot(value)
	value.Epoch = p.Epoch
	value.Sequence = p.next
	return value, nil
}

func cloneEvent(value OverlayEvent) OverlayEvent {
	if value.Receipt != nil {
		copy := *value.Receipt
		value.Receipt = &copy
	}
	if value.Live != nil {
		copy := *value.Live
		value.Live = &copy
	}
	return value
}
func cloneSnapshot(value OverlaySnapshot) OverlaySnapshot {
	value.Receipts = append([]PublicReceipt(nil), value.Receipts...)
	if value.CurrentStamp != nil {
		copy := *value.CurrentStamp
		value.CurrentStamp = &copy
	}
	return value
}
func (p *Publisher) Sequence() uint64 {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.next
}

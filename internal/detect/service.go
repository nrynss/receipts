// Package detect applies recorded or provider judge results to the ledger.
package detect

import (
	"context"
	"errors"
	"time"

	"receipts/internal/auth"
	"receipts/internal/events"
	"receipts/internal/receipt"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

type Verdict string

const (
	Contradicts Verdict = "contradicts"
	DoesNot     Verdict = "does_not_contradict"
	Unsure      Verdict = "unsure"
)

type Judge interface {
	Judge(context.Context, receipt.Receipt, receipt.Segment) (Verdict, error)
}
type Sensitive interface {
	Sensitive(context.Context, string) (bool, error)
}
type Service struct {
	Store     *store.Store
	Runtime   *channelruntime.Manager
	Judge     Judge
	Sensitive Sensitive
	Cooldown  time.Duration
	Now       func() time.Time
}

// Check accepts a candidate selected upstream; retrieval and live model calls are separate ports.
func (s Service) Check(ctx context.Context, a auth.ChannelContext, receiptID string, source receipt.Segment) error {
	if s.Store == nil || s.Runtime == nil || s.Judge == nil || s.Sensitive == nil || a.Device {
		return store.ErrInvalid
	}
	channel, err := s.Runtime.Resolve(a)
	if err != nil {
		return err
	}
	if channel.Paused() {
		return store.ErrNotEligible
	}
	if source.SessionID != a.LiveSessionID || source.Attribution != "broadcaster" || source.Text == "" {
		return store.ErrNotEligible
	}
	authority := store.DetectionAuthority{AccountID: a.AccountID, ChannelID: a.ChannelID, LiveSessionID: a.LiveSessionID, Generation: a.Generation}
	if err := s.Store.ValidateDetectionSource(ctx, authority, source); err != nil {
		return err
	}
	candidates, err := s.Store.PublicReceipts(ctx, a.ChannelID, 500)
	if err != nil {
		return err
	}
	var candidate receipt.Receipt
	for _, v := range candidates {
		if v.ID == receiptID {
			candidate = v
			break
		}
	}
	if candidate.ID == "" {
		return store.ErrNotEligible
	}
	for _, text := range []string{candidate.Claim, source.Text} {
		sensitive, err := s.Sensitive.Sensitive(ctx, text)
		if err != nil {
			return err
		}
		if sensitive {
			return store.ErrNotEligible
		}
	}
	verdict, err := s.Judge.Judge(ctx, candidate, source)
	if err != nil {
		return err
	}
	outcome := receipt.Dismissed
	switch verdict {
	case Contradicts:
		outcome = receipt.Stamp
	case Unsure:
		outcome = receipt.NeedsReview
	case DoesNot:
	default:
		return errors.New("detect: invalid judge verdict")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cooldown := s.Cooldown
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return s.Runtime.WithActive(a, func(ch *channelruntime.Channel) error {
		if ch.Paused() {
			return store.ErrNotEligible
		}
		bound, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(ch.Context, cancel)
		defer func() { stop(); cancel() }()
		ctx = bound
		apply := func() (events.OverlayEvent, error) {
			value, err := s.Store.ApplyDetection(ctx, authority, candidate, source, outcome, cooldown)
			if err != nil {
				return events.OverlayEvent{}, err
			}
			if value == nil {
				return events.OverlayEvent{}, store.ErrConflict
			}
			now := s.Now
			if now == nil {
				now = time.Now
			}
			return events.OverlayEvent{OccurredAt: now().UTC(), Receipt: value}, nil
		}
		if outcome == receipt.Stamp {
			return ch.Events.CommitError(events.Stamp, apply)
		}
		_, err := s.Store.ApplyDetection(ctx, store.DetectionAuthority{AccountID: a.AccountID, ChannelID: a.ChannelID, LiveSessionID: a.LiveSessionID, Generation: a.Generation}, candidate, source, outcome, cooldown)
		return err
	})
}

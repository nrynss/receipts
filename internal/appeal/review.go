package appeal

import (
	"context"
	"errors"
	"time"

	"receipts/internal/auth"
	"receipts/internal/events"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

type ReviewService struct {
	Store    *store.Store
	Runtime  *channelruntime.Manager
	Cooldown time.Duration
	Now      func() time.Time
}

func (s ReviewService) Resolve(ctx context.Context, a auth.ChannelContext, reviewID, key string, approve bool) error {
	if s.Store == nil || s.Runtime == nil || a.Device || reviewID == "" || key == "" || len(key) > 200 {
		return store.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	candidate, err := s.Store.ReviewCandidate(ctx, a.AccountID, a.ChannelID, reviewID)
	if err != nil {
		return err
	}
	cooldown := s.Cooldown
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	err = s.Runtime.WithActive(a, func(ch *channelruntime.Channel) error {
		bound, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(ch.Context, cancel)
		defer func() { stop(); cancel() }()
		apply := func() (events.OverlayEvent, error) {
			value, err := s.Store.ResolveReviewAction(bound, authority(a), candidate, reviewID, key, approve, ch.Paused(), cooldown)
			if err != nil {
				return events.OverlayEvent{}, err
			}
			if value.Replayed {
				return events.OverlayEvent{}, errNoChange
			}
			now := s.Now
			if now == nil {
				now = time.Now
			}
			return events.OverlayEvent{OccurredAt: now().UTC(), Receipt: value.Receipt}, nil
		}
		if approve {
			return ch.Events.CommitError(events.Stamp, apply)
		}
		_, err := apply()
		return err
	})
	if errors.Is(err, errNoChange) {
		return nil
	}
	return err
}

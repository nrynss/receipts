// Package appeal implements owner answers and trusted Twitch voting.
package appeal

import (
	"context"
	"errors"
	"strings"
	"time"

	"receipts/internal/auth"
	"receipts/internal/events"
	"receipts/internal/ports"
	"receipts/internal/receipt"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

var errNoChange = errors.New("appeal: no change")

type Service struct {
	Store    *store.Store
	Runtime  *channelruntime.Manager
	Duration time.Duration
	Now      func() time.Time
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s Service) duration() time.Duration {
	if s.Duration > 0 {
		return s.Duration
	}
	return 2 * time.Minute
}
func authority(a auth.ChannelContext) store.DetectionAuthority {
	return store.DetectionAuthority{AccountID: a.AccountID, ChannelID: a.ChannelID, LiveSessionID: a.LiveSessionID, Generation: a.Generation}
}
func (s Service) commit(ctx context.Context, a auth.ChannelContext, mutate func() (bool, error)) error {
	if s.Store == nil || s.Runtime == nil || a.Device {
		return store.ErrInvalid
	}
	err := s.Runtime.WithActive(a, func(ch *channelruntime.Channel) error {
		return ch.Events.CommitError(events.Status, func() (events.OverlayEvent, error) {
			changed, err := mutate()
			if err != nil {
				return events.OverlayEvent{}, err
			}
			if !changed {
				return events.OverlayEvent{}, errNoChange
			}
			stamp, err := s.Store.CurrentStamp(ctx, a.ChannelID, a.LiveSessionID)
			if err != nil {
				return events.OverlayEvent{}, err
			}
			current, err := s.Store.ActiveAppeal(ctx, a.ChannelID)
			if err != nil {
				return events.OverlayEvent{}, err
			}
			event := events.OverlayEvent{OccurredAt: s.now(), Receipt: stamp, Refresh: true}
			if current != nil {
				event.Appeal = &current.Public
			}
			return event, nil
		})
	})
	if errors.Is(err, errNoChange) {
		return nil
	}
	return err
}
func (s Service) Answer(ctx context.Context, a auth.ChannelContext, rid, key, kind, note string) error {
	created := false
	err := s.commit(ctx, a, func() (bool, error) {
		r, err := s.Store.ApplyReceiptAction(ctx, authority(a), rid, key, kind, note, s.duration())
		created = err == nil && !r.Replayed
		return created, err
	})
	if err == nil && created && kind == "appeal" {
		s.schedule(a)
	}
	return err
}
func (s Service) Void(ctx context.Context, a auth.ChannelContext, rid string) error {
	return s.commit(ctx, a, func() (bool, error) { return s.Store.VoidOwned(ctx, authority(a), rid) })
}
func (s Service) Vote(ctx context.Context, a auth.ChannelContext, voter string, choice receipt.VoteChoice) error {
	return s.commit(ctx, a, func() (bool, error) { return s.Store.VoteActive(ctx, authority(a), voter, choice) })
}
func (s Service) Recover(ctx context.Context, a auth.ChannelContext) error {
	return s.commit(ctx, a, func() (bool, error) { return s.Store.CloseDueAppeal(ctx, authority(a)) })
}

// StartChannel recovers overdue work and schedules an active deadline after a
// verified channel runtime starts. It never republishes a stamp animation.
func (s Service) StartChannel(ctx context.Context, a auth.ChannelContext) error {
	if err := s.Recover(ctx, a); err != nil {
		return err
	}
	s.schedule(a)
	return nil
}
func (s Service) schedule(a auth.ChannelContext) {
	ch, err := s.Runtime.Resolve(a)
	if err != nil {
		return
	}
	deadline, err := s.Store.NextAppealDeadline(ch.Context, a.ChannelID)
	if err != nil || deadline == nil {
		return
	}
	delay := deadline.Sub(s.now())
	if delay < 0 {
		delay = 0
	}
	ch.Go(func(ctx context.Context) {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if s.Recover(ctx, a) == nil {
				// If the wall clock moved backward, the deadline remains pending.
				s.schedule(a)
			}
		}
	})
}

// HandleChat accepts identity and role only from a verified Twitch adapter.
func (s Service) HandleChat(ctx context.Context, a auth.ChannelContext, cmd ports.ChatCommand) error {
	if cmd.ID == "" || cmd.UserID == "" || cmd.ChannelID != a.ChannelID || cmd.SessionID != a.LiveSessionID || cmd.ReceivedAt.IsZero() || cmd.ReceivedAt.After(s.now()) || s.now().Sub(cmd.ReceivedAt) > 30*time.Second {
		return store.ErrInvalid
	}
	switch cmd.Command {
	case "!uphold":
		if strings.TrimSpace(cmd.Arguments) != "" {
			return store.ErrInvalid
		}
		return s.Vote(ctx, a, cmd.UserID, receipt.VoteUphold)
	case "!overturn":
		if strings.TrimSpace(cmd.Arguments) != "" {
			return store.ErrInvalid
		}
		return s.Vote(ctx, a, cmd.UserID, receipt.VoteOverturn)
	case "!void":
		if cmd.Role != ports.ChatModerator && cmd.Role != ports.ChatBroadcaster {
			return auth.ErrForbidden
		}
		fields := strings.Fields(cmd.Arguments)
		if len(fields) != 1 {
			return store.ErrInvalid
		}
		return s.Void(ctx, a, fields[0])
	case "!receipts":
		if cmd.Role != ports.ChatModerator && cmd.Role != ports.ChatBroadcaster {
			return auth.ErrForbidden
		}
		switch strings.TrimSpace(cmd.Arguments) {
		case "off":
			return s.Runtime.SetPaused(a, true)
		case "on":
			return s.Runtime.SetPaused(a, false)
		default:
			return store.ErrInvalid
		}
	default:
		return store.ErrInvalid
	}
}

// ChatSink binds recorded or verified adapter commands to channel authority.
type ChatSink struct {
	Service   Service
	Authority auth.ChannelContext
}

func (s ChatSink) AcceptChatCommand(ctx context.Context, cmd ports.ChatCommand) error {
	return s.Service.HandleChat(ctx, s.Authority, cmd)
}

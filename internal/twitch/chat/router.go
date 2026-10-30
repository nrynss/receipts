// Package chat verifies EventSub chat identity and routes commands to the
// existing capture, appeal and lore services used by replay.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"receipts/internal/appeal"
	"receipts/internal/auth"
	"receipts/internal/commands"
	"receipts/internal/lore"
	"receipts/internal/ports"
)

const MaxMessageBytes = 512
const DefaultDedupeCapacity = 4096

type Capture interface {
	Handle(context.Context, commands.Authority, ports.ChatCommand) (commands.Result, error)
}
type Appeals interface {
	HandleChat(context.Context, auth.ChannelContext, ports.ChatCommand) error
}
type Lore interface {
	Handle(context.Context, string, ports.ChatCommand) (string, error)
}
type Sender interface {
	SendChatMessage(context.Context, auth.ChannelContext, string) (bool, error)
}

type Router struct {
	Authority         auth.ChannelContext
	ChannelPlatformID string
	Capture           Capture
	Appeals           Appeals
	Lore              Lore
	Sender            Sender
	Now               func() time.Time
	DedupeCapacity    int
	mu                sync.Mutex
	seen              map[string]time.Time
	order             []seenEntry
	index             int
	loreUsers         map[string]time.Time
	loreReplies       []time.Time
}
type seenEntry struct {
	id string
	at time.Time
}

type event struct {
	BroadcasterID string `json:"broadcaster_user_id"`
	ChatterID     string `json:"chatter_user_id"`
	Login         string `json:"chatter_user_login"`
	MessageID     string `json:"message_id"`
	Message       struct {
		Text string `json:"text"`
	} `json:"message"`
	Badges []struct {
		SetID string `json:"set_id"`
	} `json:"badges"`
}

// HandleEvent drops non-command chat, rejects mismatched broadcasters and
// derives role only from the payload's broadcaster identity and Twitch badges.
func (r *Router) HandleEvent(ctx context.Context, raw json.RawMessage, receivedAt time.Time) error {
	if r == nil || r.Authority.AccountID == "" || r.Authority.ChannelID == "" || r.Authority.LiveSessionID == "" || r.Authority.Generation == 0 || r.ChannelPlatformID == "" {
		return errors.New("twitch chat: verified runtime authority required")
	}
	var e event
	if len(raw) == 0 || len(raw) > MaxMessageBytes*4 || json.Unmarshal(raw, &e) != nil {
		return errors.New("twitch chat: malformed event")
	}
	if e.BroadcasterID != r.ChannelPlatformID || e.ChatterID == "" || e.MessageID == "" || len(e.Message.Text) > MaxMessageBytes {
		return errors.New("twitch chat: event does not match verified channel or size limit")
	}
	text := strings.TrimSpace(e.Message.Text)
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil
	}
	command := strings.ToLower(fields[0])
	switch command {
	case "!receipt", "!uphold", "!overturn", "!void", "!receipts", "!lore":
	default:
		return nil
	}
	if !r.admit(e.MessageID) {
		return nil
	}
	now := receivedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if r.Now != nil {
		now = r.Now().UTC()
	}
	role := ports.ChatViewer
	if e.ChatterID == r.ChannelPlatformID {
		role = ports.ChatBroadcaster
	} else {
		for _, b := range e.Badges {
			if b.SetID == "moderator" {
				role = ports.ChatModerator
				break
			}
		}
	}
	args := ""
	if len(fields) > 1 {
		args = strings.Join(fields[1:], " ")
	}
	cmd := ports.ChatCommand{ID: e.MessageID, ChannelID: r.Authority.ChannelID, SessionID: r.Authority.LiveSessionID, UserID: e.ChatterID, Login: e.Login, Role: role, Command: command, Arguments: args, ReceivedAt: now}
	switch command {
	case "!receipt":
		if r.Capture == nil {
			return errors.New("twitch chat: receipt service unavailable")
		}
		_, err := r.Capture.Handle(ctx, commands.Authority{ChannelID: r.Authority.ChannelID, SessionID: r.Authority.LiveSessionID}, cmd)
		return err
	case "!uphold", "!overturn", "!void", "!receipts":
		if r.Appeals == nil {
			return errors.New("twitch chat: appeal service unavailable")
		}
		return r.Appeals.HandleChat(ctx, r.Authority, cmd)
	case "!lore":
		if r.Lore == nil || r.Sender == nil {
			return errors.New("twitch chat: lore service unavailable")
		}
		if !r.allowLore(e.ChatterID) {
			return nil
		}
		answer, err := r.Lore.Handle(ctx, r.Authority.ChannelID, cmd)
		if err != nil {
			return err
		}
		if len(answer) > 450 {
			answer = answer[:450]
			for !utf8.ValidString(answer) {
				answer = answer[:len(answer)-1]
			}
		}
		ok, err := r.Sender.SendChatMessage(ctx, r.Authority, answer)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("twitch chat: lore reply rejected")
		}
		return nil
	}
	return nil
}

func (r *Router) admit(id string) bool {
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now().UTC()
	}
	cap := r.DedupeCapacity
	if cap <= 0 {
		cap = DefaultDedupeCapacity
	}
	if cap > 100000 {
		cap = 100000
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = map[string]time.Time{}
	}
	if at, ok := r.seen[id]; ok && now.Sub(at) < 10*time.Minute {
		return false
	}
	if len(r.order) < cap {
		r.order = append(r.order, seenEntry{id, now})
	} else {
		old := r.order[r.index]
		if r.seen[old.id] == old.at {
			delete(r.seen, old.id)
		}
		r.order[r.index] = seenEntry{id, now}
		r.index = (r.index + 1) % cap
	}
	r.seen[id] = now
	return true
}

func (r *Router) allowLore(user string) bool {
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now().UTC()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loreUsers == nil {
		r.loreUsers = map[string]time.Time{}
	}
	for i := 0; i < len(r.loreReplies); {
		if now.Sub(r.loreReplies[i]) >= 5*time.Second {
			r.loreReplies = append(r.loreReplies[:i], r.loreReplies[i+1:]...)
		} else {
			i++
		}
	}
	if last, ok := r.loreUsers[user]; ok && now.Sub(last) < 30*time.Second {
		return false
	}
	if len(r.loreReplies) >= 3 {
		return false
	}
	if len(r.loreUsers) >= 4096 {
		for k, v := range r.loreUsers {
			if now.Sub(v) >= 30*time.Second {
				delete(r.loreUsers, k)
			}
		}
		if len(r.loreUsers) >= 4096 {
			return false
		}
	}
	r.loreUsers[user] = now
	r.loreReplies = append(r.loreReplies, now)
	return true
}

var _ Capture = (*commands.Service)(nil)
var _ Appeals = appeal.Service{}
var _ Lore = lore.Service{}

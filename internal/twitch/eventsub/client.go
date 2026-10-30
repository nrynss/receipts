// Package eventsub owns the Twitch EventSub WebSocket lifecycle. It exposes
// verified messages to a chat sink and never activates a live-session runtime.
package eventsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"receipts/internal/auth"
	"receipts/internal/twitch/connection"
)

const DefaultURL = "wss://eventsub.wss.twitch.tv/ws?keepalive_timeout_seconds=30"
const MaxFrameBytes = 1 << 20

var ErrRevoked = errors.New("eventsub: subscription authorization revoked")

type Socket interface {
	Next(context.Context) ([]byte, error)
	Close() error
}
type Dialer interface {
	Dial(context.Context, string) (Socket, error)
}
type Helix interface {
	SubscribeChat(context.Context, auth.ChannelContext, string) error
}
type Sink interface {
	HandleEvent(context.Context, json.RawMessage, time.Time) error
}

type Client struct {
	Dialer              Dialer
	Helix               Helix
	Sink                Sink
	BaseURL             string
	MinBackoff          time.Duration
	MaxBackoff          time.Duration
	OnRetry             func(error, time.Duration)
	OnAuthorizationLost func(error)
	OnSubscribed        func()
}

type dialer struct{}

func (dialer) Dial(ctx context.Context, raw string) (Socket, error) {
	c, resp, err := websocket.DefaultDialer.DialContext(ctx, raw, nil)
	if err != nil {
		if resp != nil {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
			return nil, fmt.Errorf("eventsub: websocket handshake returned HTTP %d", resp.StatusCode)
		}
		return nil, errors.New("eventsub: websocket connection failed")
	}
	return &socket{c}, nil
}

type socket struct{ c *websocket.Conn }

func (s *socket) Next(ctx context.Context) ([]byte, error) {
	if d, ok := ctx.Deadline(); ok {
		_ = s.c.SetReadDeadline(d)
	}
	s.c.SetReadLimit(MaxFrameBytes)
	_, b, e := s.c.ReadMessage()
	return b, e
}
func (s *socket) Close() error { return s.c.Close() }

type Envelope struct {
	Metadata struct {
		Type      string `json:"message_type"`
		ID        string `json:"message_id"`
		Timestamp string `json:"message_timestamp"`
	} `json:"metadata"`
	Payload struct {
		Session struct {
			ID           string `json:"id"`
			Status       string `json:"status"`
			Keepalive    int    `json:"keepalive_timeout_seconds"`
			ReconnectURL string `json:"reconnect_url"`
		} `json:"session"`
		Subscription struct {
			Type      string `json:"type"`
			Status    string `json:"status"`
			Version   string `json:"version"`
			Condition struct {
				BroadcasterID string `json:"broadcaster_user_id"`
				UserID        string `json:"user_id"`
			} `json:"condition"`
			Transport struct {
				SessionID string `json:"session_id"`
			} `json:"transport"`
		} `json:"subscription"`
		Event json.RawMessage `json:"event"`
	} `json:"payload"`
}

func Parse(data []byte) (Envelope, error) {
	if len(data) == 0 || len(data) > MaxFrameBytes {
		return Envelope{}, errors.New("eventsub: frame size out of bounds")
	}
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return Envelope{}, errors.New("eventsub: malformed frame")
	}
	if e.Metadata.Type == "" {
		return Envelope{}, errors.New("eventsub: missing message type")
	}
	return e, nil
}

// Run reconnects under a previously verified channel runtime. It never creates
// runtime authority. Reconnect migrations retain the old socket until the new
// socket welcomes; ordinary disconnects resubscribe after a fresh welcome.
func (c *Client) Run(ctx context.Context, a auth.ChannelContext) error {
	if c == nil || c.Helix == nil || c.Sink == nil || a.AccountID == "" || a.ChannelID == "" || a.LiveSessionID == "" || a.Generation == 0 {
		return errors.New("eventsub: verified channel runtime and dependencies required")
	}
	d := c.Dialer
	if d == nil {
		d = dialer{}
	}
	base := c.BaseURL
	if base == "" {
		base = DefaultURL
	}
	min := c.MinBackoff
	if min <= 0 {
		min = 500 * time.Millisecond
	}
	max := c.MaxBackoff
	if max <= 0 {
		max = 15 * time.Second
	}
	if max < min {
		max = min
	}
	backoff := min
	for ctx.Err() == nil {
		err := c.session(ctx, a, d, base)
		if errors.Is(err, ErrRevoked) {
			if c.OnAuthorizationLost != nil {
				c.OnAuthorizationLost(err)
			}
			return err
		}
		if errors.Is(err, connection.ErrBinding) {
			if c.OnAuthorizationLost != nil {
				c.OnAuthorizationLost(err)
			}
			return err
		}
		if errors.Is(err, connection.ErrLost) {
			if c.OnAuthorizationLost != nil {
				c.OnAuthorizationLost(err)
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			backoff = min
		}
		if c.OnRetry != nil {
			c.OnRetry(err, backoff)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
		backoff *= 2
		if backoff > max {
			backoff = max
		}
	}
	return ctx.Err()
}

func (c *Client) session(ctx context.Context, a auth.ChannelContext, d Dialer, target string) error {
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	current, err := d.Dial(dialCtx, target)
	dialCancel()
	if err != nil {
		return err
	}
	defer func() { _ = current.Close() }()
	opening, err := welcome(ctx, current)
	if err != nil {
		return err
	}
	if err := subscribe(ctx, c.Helix, a, opening); err != nil {
		return err
	}
	if c.OnSubscribed != nil {
		c.OnSubscribed()
	}
	keepalive := seconds(opening.Payload.Session.Keepalive, 30)
	sessionID := opening.Payload.Session.ID
	for ctx.Err() == nil {
		frame, err := readFor(ctx, current, keepalive)
		if err != nil {
			return err
		}
		m, err := Parse(frame)
		if err != nil {
			return err
		}
		switch m.Metadata.Type {
		case "session_keepalive":
		case "session_reconnect":
			if !validReconnect(m.Payload.Session.ReconnectURL) {
				return errors.New("eventsub: invalid reconnect URL")
			}
			dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
			next, err := d.Dial(dialCtx, m.Payload.Session.ReconnectURL)
			dialCancel()
			if err != nil {
				return err
			}
			nw, err := welcome(ctx, next)
			if err != nil {
				_ = next.Close()
				return err
			}
			if err := c.drainPending(ctx, current, a, sessionID); err != nil {
				_ = next.Close()
				return err
			}
			_ = current.Close()
			current = next
			sessionID = nw.Payload.Session.ID
			keepalive = seconds(nw.Payload.Session.Keepalive, 30)
		case "revocation":
			if m.Payload.Subscription.Type == "channel.chat.message" && (m.Payload.Subscription.Status == "authorization_revoked" || m.Payload.Subscription.Status == "user_removed") {
				return ErrRevoked
			}
		case "notification":
			if err := c.deliver(ctx, a, sessionID, m); err != nil {
				if errors.Is(err, connection.ErrLost) {
					return err
				}
			}
		}
	}
	return ctx.Err()
}

func (c *Client) drainPending(ctx context.Context, s Socket, a auth.ChannelContext, sessionID string) error {
	// Twitch keeps the original socket alive through the replacement Welcome.
	// Drain frames already queued on that socket before switching reads.
	drainCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	for count := 0; count < 128 && drainCtx.Err() == nil; count++ {
		b, err := readFor(drainCtx, s, 250*time.Millisecond)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		m, err := Parse(b)
		if err != nil {
			continue
		}
		if m.Metadata.Type == "revocation" && m.Payload.Subscription.Type == "channel.chat.message" && (m.Payload.Subscription.Status == "authorization_revoked" || m.Payload.Subscription.Status == "user_removed") {
			return ErrRevoked
		}
		if m.Metadata.Type == "notification" {
			if err := c.deliver(ctx, a, sessionID, m); errors.Is(err, connection.ErrLost) {
				return err
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func (c *Client) deliver(ctx context.Context, a auth.ChannelContext, sessionID string, m Envelope) error {
	var identity struct {
		BroadcasterID string `json:"broadcaster_user_id"`
	}
	if len(m.Payload.Event) == 0 || json.Unmarshal(m.Payload.Event, &identity) != nil || m.Payload.Subscription.Type != "channel.chat.message" || m.Payload.Subscription.Version != "1" || m.Payload.Subscription.Status != "enabled" || m.Payload.Subscription.Condition.BroadcasterID == "" || m.Payload.Subscription.Condition.BroadcasterID != identity.BroadcasterID || m.Payload.Subscription.Condition.UserID != m.Payload.Subscription.Condition.BroadcasterID || m.Payload.Subscription.Transport.SessionID != sessionID {
		return nil
	}
	at, err := time.Parse(time.RFC3339Nano, m.Metadata.Timestamp)
	if err != nil {
		return nil
	}
	return c.Sink.HandleEvent(ctx, m.Payload.Event, at.UTC())
}
func welcome(ctx context.Context, s Socket) (Envelope, error) {
	b, err := readFor(ctx, s, 10*time.Second)
	if err != nil {
		return Envelope{}, err
	}
	m, err := Parse(b)
	if err != nil || m.Metadata.Type != "session_welcome" || m.Payload.Session.ID == "" || (m.Payload.Session.Status != "connected" && m.Payload.Session.Status != "reconnecting") {
		return Envelope{}, errors.New("eventsub: expected valid welcome")
	}
	return m, nil
}
func subscribe(ctx context.Context, h Helix, a auth.ChannelContext, m Envelope) error {
	deadline := 10 * time.Second
	if m.Payload.Session.Keepalive > 0 && time.Duration(m.Payload.Session.Keepalive)*time.Second < deadline {
		deadline = time.Duration(m.Payload.Session.Keepalive) * time.Second
	}
	subctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	return h.SubscribeChat(subctx, a, m.Payload.Session.ID)
}
func seconds(n, fallback int) time.Duration {
	if n < 1 || n > 600 {
		n = fallback
	}
	return time.Duration(n) * time.Second
}
func readFor(ctx context.Context, s Socket, limit time.Duration) ([]byte, error) {
	if limit <= 0 {
		limit = 30 * time.Second
	}
	if d, ok := ctx.Deadline(); ok && time.Until(d) < limit {
		limit = time.Until(d)
	}
	rctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	stop := context.AfterFunc(rctx, func() { _ = s.Close() })
	defer stop()
	return s.Next(rctx)
}
func validReconnect(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && strings.EqualFold(u.Scheme, "wss") && strings.EqualFold(u.Hostname(), "eventsub.wss.twitch.tv") && (u.Port() == "" || u.Port() == "443") && u.User == nil && u.Fragment == ""
}

// Adapter binds EventSub subscriptions to the TA.2-aware Helix adapter.
type Adapter struct{ Connection *connection.Adapter }

func (a Adapter) SubscribeChat(ctx context.Context, authority auth.ChannelContext, sessionID string) error {
	if a.Connection == nil {
		return errors.New("eventsub: subscription adapter unavailable")
	}
	return a.Connection.SubscribeChatVerified(ctx, authority, sessionID)
}

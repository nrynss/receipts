package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

const eventSubURL = "wss://eventsub.wss.twitch.tv/ws?keepalive_timeout_seconds=30"

type EventSubMessage struct {
	Type      string          `json:"message_type"`
	SessionID string          `json:"session_id,omitempty"`
	Event     json.RawMessage `json:"event,omitempty"`
}

func ParseEventSub(data []byte) (EventSubMessage, error) {
	var top struct {
		Metadata struct {
			Type string `json:"message_type"`
		} `json:"metadata"`
		Payload struct {
			Session struct {
				ID string `json:"id"`
			} `json:"session"`
			Event json.RawMessage `json:"event"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return EventSubMessage{}, fmt.Errorf("eventsub: invalid JSON: %w", err)
	}
	if top.Metadata.Type == "" {
		return EventSubMessage{}, errors.New("eventsub: missing message_type")
	}
	return EventSubMessage{Type: top.Metadata.Type, SessionID: top.Payload.Session.ID, Event: top.Payload.Event}, nil
}

type EventSubSocket struct{ conn *websocket.Conn }

func DialEventSub(ctx context.Context) (*EventSubSocket, error) {
	u, err := url.Parse(eventSubURL)
	if err != nil {
		return nil, errors.New("eventsub: invalid endpoint")
	}
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("eventsub: websocket dial returned HTTP %d", resp.StatusCode)
		}
		return nil, errors.New("eventsub: websocket dial failed")
	}
	return &EventSubSocket{conn: conn}, nil
}

func (s *EventSubSocket) Next(ctx context.Context) ([]byte, error) {
	if deadline, ok := ctx.Deadline(); ok {
		if err := s.conn.SetReadDeadline(deadline); err != nil {
			return nil, err
		}
	}
	_, data, err := s.conn.ReadMessage()
	return data, err
}

func (s *EventSubSocket) Close() error {
	_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	return s.conn.Close()
}

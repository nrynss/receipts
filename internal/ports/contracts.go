// Package ports defines provider and application boundaries. Adapters parse
// provider data into these inputs; services decide what those inputs mean.
package ports

import (
	"context"
	"time"
)

type TranscriptSegment struct {
	ID          string        `json:"id"`
	SessionID   string        `json:"session_id"`
	SpeakerID   string        `json:"speaker_id"`
	Attribution Attribution   `json:"attribution"`
	Start       time.Duration `json:"start_ns"`
	End         time.Duration `json:"end_ns"`
	Text        string        `json:"text"`
}

type Attribution string

const (
	AttributionBroadcaster Attribution = "broadcaster"
	AttributionUncertain   Attribution = "uncertain"
)

type ChatRole string

const (
	ChatViewer      ChatRole = "viewer"
	ChatModerator   ChatRole = "moderator"
	ChatBroadcaster ChatRole = "broadcaster"
)

type SessionState string

const (
	SessionLive    SessionState = "live"
	SessionOffline SessionState = "offline"
	SessionEnded   SessionState = "ended"
	SessionUnknown SessionState = "unknown"
)

type ChatCommand struct {
	ID         string    `json:"id"`
	ChannelID  string    `json:"channel_id"`
	SessionID  string    `json:"session_id"`
	UserID     string    `json:"user_id"`
	Login      string    `json:"login"`
	Role       ChatRole  `json:"role"`
	Command    string    `json:"command"`
	Arguments  string    `json:"arguments"`
	ReceivedAt time.Time `json:"received_at"`
}

type ClipResult struct {
	RequestID string           `json:"request_id"`
	ClipID    string           `json:"clip_id"`
	URL       string           `json:"url"`
	CreatedAt time.Time        `json:"created_at"`
	Status    ClipResultStatus `json:"status"`
}

type ClipResultStatus string

const (
	ClipPending     ClipResultStatus = "pending"
	ClipReady       ClipResultStatus = "ready"
	ClipUnavailable ClipResultStatus = "unavailable"
	ClipFailed      ClipResultStatus = "failed"
)

type LiveSessionChange struct {
	SessionID  string       `json:"session_id"`
	ChannelID  string       `json:"channel_id"`
	State      SessionState `json:"state"`
	ObservedAt time.Time    `json:"observed_at"`
	Confirmed  bool         `json:"confirmed"`
}

type TranscriptSink interface {
	AcceptTranscript(context.Context, TranscriptSegment) error
}
type ChatSink interface {
	AcceptChatCommand(context.Context, ChatCommand) error
}
type ClipSink interface {
	AcceptClipResult(context.Context, ClipResult) error
}
type SessionSink interface {
	AcceptLiveSessionChange(context.Context, LiveSessionChange) error
}

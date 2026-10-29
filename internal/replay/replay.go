// Package replay provides deterministic fixture loading and ordered dispatch
// for application services. It deliberately contains no receipt decisions.
package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"receipts/internal/ports"
)

var ErrInvalidFixture = errors.New("replay: invalid fixture")

// Fixture is an ordered recording of typed adapter outputs. Only one payload
// is populated for each Kind; service-owned model/clip responses are deferred
// until those application service ports exist.
type Fixture struct {
	Version int     `json:"version"`
	Inputs  []Input `json:"inputs"`
}
type Input struct {
	At         time.Time                `json:"at"`
	Kind       string                   `json:"kind"`
	Transcript *ports.TranscriptSegment `json:"transcript,omitempty"`
	Chat       *ports.ChatCommand       `json:"chat,omitempty"`
	Clip       *ports.ClipResult        `json:"clip,omitempty"`
	Session    *ports.LiveSessionChange `json:"session,omitempty"`
}
type Sinks struct {
	Transcript ports.TranscriptSink
	Chat       ports.ChatSink
	Clip       ports.ClipSink
	Session    ports.SessionSink
}

// Clock advances only when the ordered fixture advances, never using wall time.
type Clock struct{ current time.Time }

func NewClock(start time.Time) *Clock { return &Clock{current: start.UTC()} }
func (c *Clock) Now() time.Time       { return c.current }
func (c *Clock) Advance(at time.Time) error {
	at = at.UTC()
	if at.IsZero() || at.Before(c.current) {
		return fmt.Errorf("%w: timestamps must be nonzero and ordered", ErrInvalidFixture)
	}
	c.current = at
	return nil
}

// IDs returns a sequential Keel-shaped UUID source, local to one replay run.
func IDs() func() (string, error) {
	var n uint64
	var mu sync.Mutex
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprintf("%032x", n), nil
	}
}

func Decode(r io.Reader) (Fixture, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var f Fixture
	if err := dec.Decode(&f); err != nil {
		return Fixture{}, fmt.Errorf("%w: %v", ErrInvalidFixture, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Fixture{}, fmt.Errorf("%w: trailing JSON data", ErrInvalidFixture)
	}
	if err := f.Validate(); err != nil {
		return Fixture{}, err
	}
	return f, nil
}

func (f Fixture) Validate() error {
	if f.Version != 1 || len(f.Inputs) == 0 {
		return fmt.Errorf("%w: version 1 and at least one input are required", ErrInvalidFixture)
	}
	previous := time.Time{}
	for i, in := range f.Inputs {
		if in.At.IsZero() || (!previous.IsZero() && in.At.Before(previous)) {
			return fmt.Errorf("%w: input %d timestamp is missing or out of order", ErrInvalidFixture, i)
		}
		previous = in.At
		count := 0
		if in.Transcript != nil {
			count++
			if in.Kind != "transcript" {
				return fmt.Errorf("%w: input %d kind/payload mismatch", ErrInvalidFixture, i)
			}
		}
		if in.Chat != nil {
			count++
			if in.Kind != "chat" {
				return fmt.Errorf("%w: input %d kind/payload mismatch", ErrInvalidFixture, i)
			}
		}
		if in.Clip != nil {
			count++
			if in.Kind != "clip" {
				return fmt.Errorf("%w: input %d kind/payload mismatch", ErrInvalidFixture, i)
			}
		}
		if in.Session != nil {
			count++
			if in.Kind != "session" {
				return fmt.Errorf("%w: input %d kind/payload mismatch", ErrInvalidFixture, i)
			}
		}
		if count != 1 {
			return fmt.Errorf("%w: input %d must contain exactly one typed payload", ErrInvalidFixture, i)
		}
		if err := validateInput(in); err != nil {
			return fmt.Errorf("%w: input %d: %v", ErrInvalidFixture, i, err)
		}
	}
	return nil
}

func required(value, name string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func validateInput(in Input) error {
	switch in.Kind {
	case "transcript":
		v := in.Transcript
		if err := required(v.ID, "transcript.id"); err != nil {
			return err
		}
		if err := required(v.SessionID, "transcript.session_id"); err != nil {
			return err
		}
		if err := required(v.Text, "transcript.text"); err != nil {
			return err
		}
		if v.Start < 0 || v.End < v.Start {
			return errors.New("transcript offsets are invalid")
		}
		if v.Attribution != ports.AttributionBroadcaster && v.Attribution != ports.AttributionUncertain {
			return errors.New("transcript attribution is invalid")
		}
	case "chat":
		v := in.Chat
		for _, field := range []struct{ value, name string }{{v.ID, "chat.id"}, {v.ChannelID, "chat.channel_id"}, {v.SessionID, "chat.session_id"}, {v.UserID, "chat.user_id"}, {v.Login, "chat.login"}, {v.Command, "chat.command"}} {
			if err := required(field.value, field.name); err != nil {
				return err
			}
		}
		if v.ReceivedAt.IsZero() {
			return errors.New("chat.received_at is required")
		}
		if v.Role != ports.ChatViewer && v.Role != ports.ChatModerator && v.Role != ports.ChatBroadcaster {
			return errors.New("chat role is invalid")
		}
	case "clip":
		v := in.Clip
		if err := required(v.RequestID, "clip.request_id"); err != nil {
			return err
		}
		if v.CreatedAt.IsZero() {
			return errors.New("clip.created_at is required")
		}
		switch v.Status {
		case ports.ClipPending, ports.ClipUnavailable, ports.ClipFailed:
		case ports.ClipReady:
			if err := required(v.ClipID, "clip.clip_id"); err != nil {
				return err
			}
			if err := required(v.URL, "clip.url"); err != nil {
				return err
			}
		default:
			return errors.New("clip status is invalid")
		}
	case "session":
		v := in.Session
		if err := required(v.SessionID, "session.session_id"); err != nil {
			return err
		}
		if err := required(v.ChannelID, "session.channel_id"); err != nil {
			return err
		}
		if v.ObservedAt.IsZero() {
			return errors.New("session.observed_at is required")
		}
		switch v.State {
		case ports.SessionLive, ports.SessionOffline, ports.SessionEnded, ports.SessionUnknown:
		default:
			return errors.New("session state is invalid")
		}
	default:
		return fmt.Errorf("unknown kind %q", in.Kind)
	}
	return nil
}

// Run dispatches recorded adapter inputs in their declared order. It never
// creates provider clients; missing boundaries fail closed.
func (f Fixture) Run(ctx context.Context, sinks Sinks, clock *Clock) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if clock == nil {
		return fmt.Errorf("%w: clock is required", ErrInvalidFixture)
	}
	if len(f.Inputs) > 0 && f.Inputs[0].At.Before(clock.Now()) {
		return fmt.Errorf("%w: clock begins after first input", ErrInvalidFixture)
	}
	// Resolve every required sink before any callback can mutate application state.
	for _, in := range f.Inputs {
		switch in.Kind {
		case "transcript":
			if sinks.Transcript == nil {
				return missingSink("transcript")
			}
		case "chat":
			if sinks.Chat == nil {
				return missingSink("chat")
			}
		case "clip":
			if sinks.Clip == nil {
				return missingSink("clip")
			}
		case "session":
			if sinks.Session == nil {
				return missingSink("session")
			}
		}
	}
	for i, in := range f.Inputs {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("replay input %d canceled: %w", i, err)
		}
		if err := clock.Advance(in.At); err != nil {
			return err
		}
		var err error
		switch in.Kind {
		case "transcript":
			err = sinks.Transcript.AcceptTranscript(ctx, *in.Transcript)
		case "chat":
			err = sinks.Chat.AcceptChatCommand(ctx, *in.Chat)
		case "clip":
			err = sinks.Clip.AcceptClipResult(ctx, *in.Clip)
		case "session":
			err = sinks.Session.AcceptLiveSessionChange(ctx, *in.Session)
		default:
			err = fmt.Errorf("%w: unknown kind %q", ErrInvalidFixture, in.Kind)
		}
		if err != nil {
			return fmt.Errorf("replay input %d (%s): %w", i, in.Kind, err)
		}
	}
	return nil
}
func missingSink(kind string) error {
	return fmt.Errorf("%w: %s sink is required", ErrInvalidFixture, kind)
}

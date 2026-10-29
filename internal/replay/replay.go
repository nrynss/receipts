// Package replay provides deterministic fixture loading and ordered dispatch
// for application services. It deliberately contains no receipt decisions.
package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	for i, in := range f.Inputs {
		if err := clock.Advance(in.At); err != nil {
			return err
		}
		var err error
		switch in.Kind {
		case "transcript":
			if sinks.Transcript == nil {
				err = missingSink("transcript")
			} else {
				err = sinks.Transcript.AcceptTranscript(ctx, *in.Transcript)
			}
		case "chat":
			if sinks.Chat == nil {
				err = missingSink("chat")
			} else {
				err = sinks.Chat.AcceptChatCommand(ctx, *in.Chat)
			}
		case "clip":
			if sinks.Clip == nil {
				err = missingSink("clip")
			} else {
				err = sinks.Clip.AcceptClipResult(ctx, *in.Clip)
			}
		case "session":
			if sinks.Session == nil {
				err = missingSink("session")
			} else {
				err = sinks.Session.AcceptLiveSessionChange(ctx, *in.Session)
			}
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

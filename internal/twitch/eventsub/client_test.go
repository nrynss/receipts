package eventsub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"receipts/internal/auth"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile("../../../testdata/twitch/eventsub/" + name + ".json")
	if e != nil {
		t.Fatal(e)
	}
	return b
}

type memSocket struct {
	frames [][]byte
	mu     sync.Mutex
	closed bool
}

func (s *memSocket) Next(ctx context.Context) ([]byte, error) {
	if len(s.frames) == 0 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	b := s.frames[0]
	s.frames = s.frames[1:]
	return b, nil
}
func (s *memSocket) Close() error   { s.mu.Lock(); defer s.mu.Unlock(); s.closed = true; return nil }
func (s *memSocket) isClosed() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.closed }

type memDial struct {
	sockets []*memSocket
	urls    []string
}

type failingDial struct{}

func (failingDial) Dial(context.Context, string) (Socket, error) {
	return nil, errors.New("temporary dial failure")
}

func (d *memDial) Dial(_ context.Context, u string) (Socket, error) {
	d.urls = append(d.urls, u)
	if len(d.sockets) == 0 {
		return nil, errors.New("no socket")
	}
	s := d.sockets[0]
	d.sockets = d.sockets[1:]
	return s, nil
}

type memHelix struct {
	calls   int
	session string
}

func (h *memHelix) SubscribeChat(_ context.Context, a auth.ChannelContext, s string) error {
	h.calls++
	h.session = s
	if a.LiveSessionID == "" {
		return errors.New("untrusted runtime")
	}
	return nil
}

type memSink struct {
	calls int
	at    time.Time
}

func (s *memSink) HandleEvent(_ context.Context, _ json.RawMessage, at time.Time) error {
	s.calls++
	s.at = at
	return nil
}

func TestReconnectPreservesSubscriptionAndConsumesExactlyOnce(t *testing.T) {
	old := &memSocket{frames: [][]byte{fixture(t, "welcome"), fixture(t, "keepalive"), fixture(t, "reconnect")}}
	new := &memSocket{frames: [][]byte{fixture(t, "welcome"), fixture(t, "notification"), fixture(t, "revocation")}}
	d := &memDial{sockets: []*memSocket{old, new}}
	h := &memHelix{}
	sink := &memSink{}
	c := &Client{Dialer: d, Helix: h, Sink: sink}
	a := auth.ChannelContext{AccountID: "account", ChannelID: "channel", LiveSessionID: "live", Generation: 1}
	err := c.session(context.Background(), a, d, "wss://eventsub.wss.twitch.tv/ws")
	if !errors.Is(err, ErrRevoked) {
		t.Fatalf("session err=%v", err)
	}
	if h.calls != 1 || h.session != "event-session-1" {
		t.Fatalf("subscription calls=%d session=%q", h.calls, h.session)
	}
	if sink.calls != 1 || !sink.at.Equal(time.Date(2026, 10, 30, 0, 0, 3, 123456000, time.UTC)) {
		t.Fatalf("sink calls=%d at=%s", sink.calls, sink.at)
	}
	if len(d.urls) != 2 || d.urls[1] != "wss://eventsub.wss.twitch.tv/ws?keepalive_timeout_seconds=30" {
		t.Fatalf("dial URLs=%v", d.urls)
	}
	if !old.isClosed() || !new.isClosed() {
		t.Fatalf("sockets not closed old=%v new=%v", old.isClosed(), new.isClosed())
	}
}

func TestRejectsUnsafeReconnectAndOversizeFrames(t *testing.T) {
	for _, u := range []string{"https://eventsub.wss.twitch.tv/x", "wss://evil.example/x", "wss://user@eventsub.wss.twitch.tv/x", "wss://eventsub.wss.twitch.tv:8443/ws"} {
		if validReconnect(u) {
			t.Fatalf("accepted reconnect URL %q", u)
		}
	}
	if validReconnect("wss://eventsub.wss.twitch.tv/ws?token=opaque") == false {
		t.Fatal("rejected official reconnect URL")
	}
	if _, err := Parse(make([]byte, MaxFrameBytes+1)); err == nil {
		t.Fatal("accepted oversized frame")
	}
}

func TestRunRequiresRuntimeAndStopsOnCancellation(t *testing.T) {
	c := &Client{Helix: &memHelix{}, Sink: &memSink{}}
	if err := c.Run(context.Background(), auth.ChannelContext{}); err == nil {
		t.Fatal("missing runtime accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.Run(ctx, auth.ChannelContext{AccountID: "a", ChannelID: "c", LiveSessionID: "s", Generation: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestRunReportsCappedExponentialRetryDelays(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := auth.ChannelContext{AccountID: "a", ChannelID: "c", LiveSessionID: "s", Generation: 1}
	var delays []time.Duration
	c := &Client{Dialer: failingDial{}, Helix: &memHelix{}, Sink: &memSink{}, MinBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond, OnRetry: func(_ error, d time.Duration) {
		delays = append(delays, d)
		if len(delays) == 3 {
			cancel()
		}
	}}
	err := c.Run(ctx, a)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error=%v", err)
	}
	want := []time.Duration{time.Millisecond, 2 * time.Millisecond, 2 * time.Millisecond}
	if len(delays) != len(want) {
		t.Fatalf("delays=%v", delays)
	}
	for i := range want {
		if delays[i] != want[i] {
			t.Fatalf("delays=%v want=%v", delays, want)
		}
	}
}

func TestRuntimeCancellationClosesBlockedSocketRead(t *testing.T) {
	sock := &memSocket{frames: [][]byte{fixture(t, "welcome")}}
	d := &memDial{sockets: []*memSocket{sock}}
	ready := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	a := auth.ChannelContext{AccountID: "a", ChannelID: "c", LiveSessionID: "s", Generation: 1}
	c := &Client{Dialer: d, Helix: &memHelix{}, Sink: &memSink{}, OnSubscribed: func() { close(ready) }}
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, a) }()
	select {
	case <-ready:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("subscription did not complete")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime cancellation did not stop socket reader")
	}
	if !sock.isClosed() {
		t.Fatal("canceled runtime left socket open")
	}
}

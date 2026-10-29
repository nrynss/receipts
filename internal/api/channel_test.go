package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"receipts/internal/auth"
	"receipts/internal/events"
	channelruntime "receipts/internal/runtime"
)

type syncedWriter struct {
	header  http.Header
	mu      sync.Mutex
	body    bytes.Buffer
	changed chan struct{}
}

func newSyncedWriter() *syncedWriter {
	return &syncedWriter{header: make(http.Header), changed: make(chan struct{}, 1)}
}
func (w *syncedWriter) Header() http.Header { return w.header }
func (w *syncedWriter) WriteHeader(int)     {}
func (w *syncedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	n, e := w.body.Write(p)
	w.mu.Unlock()
	select {
	case w.changed <- struct{}{}:
	default:
	}
	return n, e
}
func (w *syncedWriter) Flush() {
	select {
	case w.changed <- struct{}{}:
	default:
	}
}
func (w *syncedWriter) Text() string { w.mu.Lock(); defer w.mu.Unlock(); return w.body.String() }

type channelReader struct{}

func (channelReader) OverlaySnapshotFor(_ *http.Request, c auth.ChannelContext) (events.OverlaySnapshot, error) {
	return events.OverlaySnapshot{Channel: c.ChannelID}, nil
}
func (channelReader) ReviewQueueFor(_ *http.Request, _ auth.ChannelContext) ([]events.ReviewItem, error) {
	return nil, nil
}
func (channelReader) HealthFor(_ *http.Request, _ auth.ChannelContext) (events.Health, error) {
	return events.Health{}, nil
}

func TestHostedOverlayUsesValidatedChannelAndNeverGlobalReader(t *testing.T) {
	manager := channelruntime.New(channelruntime.Config{MaxChannels: 2})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", LiveSessionID: "live-a", Generation: 1}
	b := auth.ChannelContext{AccountID: "owner-b", ChannelID: "channel-b", LiveSessionID: "live-b", Generation: 1}
	if _, err := manager.Start(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	s := New(Config{Reader: fixtureReader{}, OwnerReader: channelReader{}})
	s.requireChannel = true
	handler := manager.Middleware(s)
	req := httptest.NewRequest(http.MethodGet, "/api/overlay?channel_id=channel-b", nil)
	req = req.WithContext(auth.WithChannelContext(req.Context(), auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a"}))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	var snapshot events.OverlaySnapshot
	if err := json.Unmarshal(res.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if res.Code != http.StatusOK || snapshot.Channel != "channel-a" {
		t.Fatalf("cross-channel snapshot: %d %+v", res.Code, snapshot)
	}
}

func TestHostedEventsReplayIsChannelLocalAndStopCancelsSSE(t *testing.T) {
	manager := channelruntime.New(channelruntime.Config{MaxChannels: 2})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", LiveSessionID: "live-a", Generation: 1}
	b := auth.ChannelContext{AccountID: "owner-b", ChannelID: "channel-b", LiveSessionID: "live-b", Generation: 1}
	ca, err := manager.Start(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := manager.Start(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	ca.Events.Commit("stamp", func() events.OverlayEvent { return events.OverlayEvent{OccurredAt: time.Now()} })
	cb.Events.Commit("stamp", func() events.OverlayEvent { return events.OverlayEvent{OccurredAt: time.Now()} })
	s := New(Config{})
	s.requireChannel = true
	handler := manager.Middleware(s)
	ctx, cancel := context.WithCancel(auth.WithChannelContext(context.Background(), a))
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	res := newSyncedWriter()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(res, req); close(done) }()
	deadline := time.After(time.Second)
	for !bytes.Contains([]byte(res.Text()), []byte("reset")) {
		select {
		case <-res.changed:
		case <-deadline:
			t.Fatal("SSE reset frame not written")
		}
	}
	ca.Events.Commit("stamp", func() events.OverlayEvent { return events.OverlayEvent{OccurredAt: time.Now()} })
	deadline = time.After(time.Second)
	for !strings.Contains(res.Text(), ca.Epoch) {
		select {
		case <-res.changed:
		case <-deadline:
			t.Fatal("channel A event not written")
		}
	}
	body := res.Text()
	if !bytes.Contains([]byte(body), []byte(ca.Epoch)) || bytes.Contains([]byte(body), []byte(cb.Epoch)) {
		t.Fatalf("SSE replay crossed channels: %s", body)
	}
	if ca.Events == cb.Events {
		t.Fatal("publishers are shared")
	}
	if err := manager.Stop(a); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runtime stop did not close SSE")
	}
	cancel()
	if _, err := manager.Resolve(b); err != nil {
		t.Fatalf("A stop affected B: %v", err)
	}
}

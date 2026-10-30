package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type eventHandlerFunc func(context.Context, json.RawMessage, time.Time) error

func (f eventHandlerFunc) HandleEvent(ctx context.Context, raw json.RawMessage, at time.Time) error {
	return f(ctx, raw, at)
}

func TestCaptureQueueDoesNotBlockReaderAndDropsOverflow(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var events []queuedEvent
	sink := newCaptureSink(context.Background(), eventHandlerFunc(func(ctx context.Context, raw json.RawMessage, at time.Time) error {
		mu.Lock()
		events = append(events, queuedEvent{raw: append(json.RawMessage(nil), raw...), at: at})
		n := len(events)
		mu.Unlock()
		if n == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}), 1)
	defer sink.stop()
	at := time.Unix(123, 456)
	if err := sink.HandleEvent(context.Background(), json.RawMessage(`{"id":1,"message":{"text":"!receipt"}}`), at); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	second := json.RawMessage(`{"id":2,"message":{"text":"!receipt"}}`)
	readerDone := make(chan struct{})
	go func() {
		_ = sink.HandleEvent(context.Background(), second, at.Add(time.Second))
		_ = sink.HandleEvent(context.Background(), json.RawMessage(`{"id":3,"message":{"text":"!receipt"}}`), at.Add(2*time.Second))
		close(readerDone)
	}()
	select {
	case <-readerDone:
	case <-time.After(time.Second):
		t.Fatal("reader blocked on capture or full queue")
	}
	second[6] = '9'
	close(release)
	deadline := time.After(time.Second)
	for {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("queued event was not processed")
		case <-time.After(time.Millisecond):
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if string(events[1].raw) != `{"id":2,"message":{"text":"!receipt"}}` || !events[1].at.Equal(at.Add(time.Second)) {
		t.Fatalf("queued copy/time: %#v", events[1])
	}
	if len(events) != 2 {
		t.Fatalf("overflow processed: %d", len(events))
	}
}

func TestCaptureQueueCancellationDiscardsPending(t *testing.T) {
	entered := make(chan struct{})
	canceled := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	sink := newCaptureSink(context.Background(), eventHandlerFunc(func(ctx context.Context, _ json.RawMessage, _ time.Time) error {
		mu.Lock()
		calls++
		mu.Unlock()
		close(entered)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	}), 2)
	_ = sink.HandleEvent(context.Background(), json.RawMessage(`{"id":1,"message":{"text":"!receipt"}}`), time.Now())
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	_ = sink.HandleEvent(context.Background(), json.RawMessage(`{"id":2,"message":{"text":"!receipt"}}`), time.Now())
	done := make(chan struct{})
	go func() { sink.stop(); close(done) }()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("in-flight capture was not canceled")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop did not drain")
	}
	_ = sink.HandleEvent(context.Background(), json.RawMessage(`{"id":3,"message":{"text":"!receipt"}}`), time.Now())
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("processed %d events after cancellation", calls)
	}
}

package main

import (
	"context"
	"testing"
	"time"

	"receipts/internal/auth"
	"receipts/internal/receipt"
)

func TestJudgeQueueBoundedAndDiscardsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := auth.ChannelContext{AccountID: "owner", ChannelID: "channel", LiveSessionID: "live", Generation: 1}
	entered := make(chan string, 2)
	release := make(chan struct{})
	q := newJudgeQueue(ctx, a, 1, func(_ context.Context, s receipt.Segment) error { entered <- s.ID; <-release; return nil })
	q.enqueue(a, receipt.Segment{ID: "first"})
	select {
	case got := <-entered:
		if got != "first" {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	wrong := a
	wrong.AccountID = "other"
	q.enqueue(wrong, receipt.Segment{ID: "foreign"})
	stale := a
	stale.Generation++
	q.enqueue(stale, receipt.Segment{ID: "stale"})
	q.enqueue(a, receipt.Segment{ID: "queued"})
	finished := make(chan struct{})
	go func() { q.enqueue(a, receipt.Segment{ID: "overflow"}); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("overflow blocked")
	}
	if len(q.segments) != 1 {
		t.Fatalf("queue length=%d", len(q.segments))
	}
	cancel()
	close(release)
	q.wait()
	select {
	case s := <-entered:
		t.Fatalf("processed queued or unauthorized segment %s", s)
	default:
	}
	remaining := len(q.segments)
	q.enqueue(a, receipt.Segment{ID: "after-cancel"})
	if len(q.segments) != remaining {
		t.Fatal("accepted after cancellation")
	}
}

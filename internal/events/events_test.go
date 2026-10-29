package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/stream"
)

func TestCommitErrorPublishesNothingOnFailedMutation(t *testing.T) {
	p := NewPublisher("epoch")
	errWant := errors.New("store refused")
	if err := p.CommitError(Status, func() (OverlayEvent, error) { return OverlayEvent{}, errWant }); !errors.Is(err, errWant) {
		t.Fatalf("CommitError=%v", err)
	}
	if p.Sequence() != 0 {
		t.Fatalf("failed mutation advanced sequence to %d", p.Sequence())
	}
}

func TestConcurrentCommitSequenceIsOrderedAndPayloadsAreOwned(t *testing.T) {
	const count = 32
	p := NewPublisher("epoch")
	p.Broker = stream.New(stream.Config{Buffer: count + 2, Replay: count + 2})
	sub := p.Broker.Subscribe(context.Background(), TopicOverlay)
	defer sub.Cancel()
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Commit(Stamp, func() OverlayEvent { return OverlayEvent{OccurredAt: time.Unix(1, 0)} })
		}()
	}
	wg.Wait()
	for want := uint64(1); want <= count; want++ {
		event := <-sub.Events
		data, ok := event.Data.(OverlayEvent)
		if !ok {
			t.Fatalf("event data type %T", event.Data)
		}
		if data.Sequence != want {
			t.Fatalf("sequence=%d want %d", data.Sequence, want)
		}
	}
	receipt := PublicReceipt{ID: "original", Claim: "first"}
	live := true
	p.Commit(Stamp, func() OverlayEvent { return OverlayEvent{Receipt: &receipt, Live: &live} })
	receipt.ID = "mutated"
	live = false
	data := (<-sub.Events).Data.(OverlayEvent)
	if data.Receipt.ID != "original" || !*data.Live {
		t.Fatalf("published payload aliased caller data: %+v", data)
	}
	original := OverlaySnapshot{Receipts: []PublicReceipt{{ID: "before"}}, CurrentStamp: &PublicReceipt{ID: "current"}}
	snapshot, err := p.Snapshot(func() (OverlaySnapshot, error) { return original, nil })
	if err != nil {
		t.Fatal(err)
	}
	original.Receipts[0].ID = "after"
	original.CurrentStamp.ID = "changed"
	if snapshot.Receipts[0].ID != "before" || snapshot.CurrentStamp.ID != "current" {
		t.Fatalf("snapshot aliased read model: %+v", snapshot)
	}
}

func TestSnapshotWaitsForCommitAndCarriesItsSequence(t *testing.T) {
	p := NewPublisher("epoch")
	entered := make(chan struct{})
	release := make(chan struct{})
	committed := make(chan struct{})
	go func() {
		p.Commit(Stamp, func() OverlayEvent { close(entered); <-release; return OverlayEvent{} })
		close(committed)
	}()
	<-entered
	snapshotDone := make(chan OverlaySnapshot, 1)
	go func() {
		value, _ := p.Snapshot(func() (OverlaySnapshot, error) { return OverlaySnapshot{Channel: "demo"}, nil })
		snapshotDone <- value
	}()
	select {
	case <-snapshotDone:
		t.Fatal("snapshot crossed an in-progress commit")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-committed
	value := <-snapshotDone
	if value.Sequence != 1 || value.Epoch != "epoch" {
		t.Fatalf("snapshot version = (%s,%d)", value.Epoch, value.Sequence)
	}
}

package audio

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveMonitorStopsAndWaitsOnLostConfirmation(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "offline", true: "unknown"}[unknown], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var checks atomic.Int32
			cleaned := false
			capture := func(ctx context.Context) error { <-ctx.Done(); cleaned = true; return ctx.Err() }
			check := func(context.Context) (bool, error) {
				if checks.Add(1) == 1 {
					return true, nil
				}
				if unknown {
					return false, errors.New("unavailable")
				}
				return false, nil
			}
			err := CaptureWhileLive(ctx, check, capture, time.Millisecond)
			want := ErrOffline
			if unknown {
				want = ErrLiveUnknown
			}
			if !errors.Is(err, want) || !cleaned {
				t.Fatalf("err=%v, capture cleanup completed=%v", err, cleaned)
			}
		})
	}
}

func TestLiveMonitorRefusesCaptureWithoutInitialConfirmation(t *testing.T) {
	called := false
	err := CaptureWhileLive(context.Background(), func(context.Context) (bool, error) { return false, nil }, func(context.Context) error { called = true; return nil }, time.Second)
	if !errors.Is(err, ErrOffline) || called {
		t.Fatal("offline startup must not invoke capture")
	}
}

func TestLiveMonitorReportsSourceEndWithoutResuming(t *testing.T) {
	calls := 0
	err := CaptureWhileLive(context.Background(), func(context.Context) (bool, error) { return true, nil }, func(context.Context) error { calls++; return ErrSourceEnded }, time.Second)
	if !errors.Is(err, ErrSourceEnded) || calls != 1 {
		t.Fatalf("err=%v, capture calls=%d", err, calls)
	}
}

func TestCancellationDuringLiveCheckKeepsCancellationReason(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	checking := make(chan struct{})
	var checks atomic.Int32
	check := func(c context.Context) (bool, error) {
		if checks.Add(1) == 1 {
			return true, nil
		}
		close(checking)
		<-c.Done()
		return false, c.Err()
	}
	done := make(chan error, 1)
	go func() {
		done <- CaptureWhileLive(ctx, check, func(c context.Context) error { <-c.Done(); return c.Err() }, time.Millisecond)
	}()
	<-checking
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was mislabeled: %v", err)
	}
}

func TestContinuousCaptureRequiresDeadline(t *testing.T) {
	if err := CaptureUntilCanceled(context.Background(), "unavailable", "", ""); err == nil {
		t.Fatal("unbounded continuous capture must be refused before starting a process")
	}
}

package audio

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrOffline = errors.New("broadcast is offline")
var ErrLiveUnknown = errors.New("live confirmation is unavailable")

// CaptureWhileLive checks before starting and periodically while capture runs.
// Any lost confirmation cancels capture and waits for its cleanup. It never
// retries or resumes automatically after offline or unknown status.
func CaptureWhileLive(ctx context.Context, check func(context.Context) (bool, error), capture func(context.Context) error, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("live check interval must be positive")
	}
	live, err := check(ctx)
	if err != nil {
		return ErrLiveUnknown
	}
	if !live {
		return ErrOffline
	}
	captureCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ended := make(chan error, 1)
	go func() { ended <- capture(captureCtx) }()
	stop := func(reason error) error {
		cancel()
		<-ended // Capture returns only after its subprocesses have been reaped.
		return reason
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return stop(ctx.Err())
		case err := <-ended:
			if err == nil {
				return ErrSourceEnded
			}
			return fmt.Errorf("capture stopped: %w", err)
		case <-ticker.C:
			live, err := check(ctx)
			if ctx.Err() != nil {
				return stop(ctx.Err())
			}
			if err != nil {
				return stop(ErrLiveUnknown)
			}
			if !live {
				return stop(ErrOffline)
			}
		}
	}
}

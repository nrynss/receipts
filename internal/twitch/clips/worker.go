// Package clips processes durable Twitch clip jobs without changing receipt
// capture semantics. Transcript evidence remains authoritative if video is late.
package clips

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"receipts/internal/auth"
	"receipts/internal/events"
	"receipts/internal/receipt"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
	"receipts/internal/twitch"
	"receipts/internal/twitch/connection"
)

const readinessWindow = 60 * time.Second

type Ledger interface {
	ClaimDueClipJobs(context.Context, store.DetectionAuthority, int, time.Duration) ([]receipt.ClipJob, error)
	MarkClipCreateStarted(context.Context, store.DetectionAuthority, receipt.ClipJob) error
	ResetClipCreateStarted(context.Context, store.DetectionAuthority, receipt.ClipJob) error
	SaveClipJobAttempt(context.Context, store.DetectionAuthority, receipt.ClipJob, string, string, time.Time) error
	CompleteClipJobBound(context.Context, store.DetectionAuthority, receipt.ClipJob, string, string, string, string) error
}
type Twitch interface {
	CreateClip(context.Context, auth.ChannelContext) (string, error)
	GetClip(context.Context, auth.ChannelContext, string) (connection.ClipReadiness, error)
}
type Worker struct {
	Ledger       Ledger
	Twitch       Twitch
	Runtime      *channelruntime.Manager
	Now          func() time.Time
	PollInterval time.Duration
	OnError      func(error)
}

func (w *Worker) Enqueue(_ context.Context, channelID, commandID, receiptID string, _ time.Time) error {
	if w == nil || w.Ledger == nil || channelID == "" || commandID == "" || receiptID == "" {
		return store.ErrInvalid
	}
	return nil // CaptureReceipt already inserted the durable job transactionally.
}
func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}
func authority(a auth.ChannelContext) store.DetectionAuthority {
	return store.DetectionAuthority{AccountID: a.AccountID, ChannelID: a.ChannelID, LiveSessionID: a.LiveSessionID, Generation: a.Generation}
}

func (w *Worker) Run(ctx context.Context, a auth.ChannelContext) error {
	if w == nil || w.Ledger == nil || w.Twitch == nil || w.Runtime == nil || a.ChannelID == "" {
		return store.ErrInvalid
	}
	ticker := time.NewTicker(w.PollIntervalOrDefault())
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx, a); err != nil && !errors.Is(err, context.Canceled) {
			if w.OnError != nil {
				w.OnError(err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Attach registers one runner with the currently active channel runtime. The
// runtime drains it on stop; a later runtime can recover unexpired job leases.
func (w *Worker) Attach(a auth.ChannelContext) bool {
	_, ok := w.AttachAndWait(a)
	return ok
}

// AttachAndWait registers one runner with the current channel generation and
// returns a join function. Call the join function after cancelling the runtime
// to ensure no clip request or event publication remains in flight.
func (w *Worker) AttachAndWait(a auth.ChannelContext) (func(), bool) {
	if w == nil || w.Runtime == nil {
		return nil, false
	}
	ch, err := w.Runtime.Resolve(a)
	if err != nil {
		return nil, false
	}
	done := make(chan struct{})
	if !ch.Go(func(ctx context.Context) {
		defer close(done)
		_ = w.Run(ctx, a)
	}) {
		return nil, false
	}
	return func() { <-done }, true
}

func (w *Worker) RunOnce(ctx context.Context, a auth.ChannelContext) error {
	if w == nil || w.Ledger == nil || w.Twitch == nil || w.Runtime == nil {
		return store.ErrInvalid
	}
	var jobs []receipt.ClipJob
	err := w.Runtime.WithActive(a, func(*channelruntime.Channel) error {
		var e error
		jobs, e = w.Ledger.ClaimDueClipJobs(ctx, authority(a), 1, 15*time.Second)
		return e
	})
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = w.process(ctx, a, job); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, channelruntime.ErrStale) && !errors.Is(err, store.ErrNotEligible) {
			return err
		}
	}
	return nil
}
func (w *Worker) process(ctx context.Context, a auth.ChannelContext, job receipt.ClipJob) error {
	if job.ChannelID != a.ChannelID || job.CommandID == "" {
		return store.ErrConflict
	}
	if job.ClipID == "" {
		if job.CreateStarted {
			return w.fail(ctx, a, job, "", "clip creation result unknown after worker recovery; transcript evidence retained")
		}
		if err := w.markCreateStarted(ctx, a, job); err != nil {
			return err
		}
		job.CreateStarted = true
		job.ClipRequestedAt = w.now()
		if !job.EvidenceEndsAt.IsZero() {
			job.CaptureLag = job.ClipRequestedAt.Sub(job.EvidenceEndsAt)
		}
		postCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		id, err := w.Twitch.CreateClip(postCtx, a)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			var h *twitch.HTTPError
			if errors.As(err, &h) && h.StatusCode == http.StatusTooManyRequests {
				if resetErr := w.resetCreateStarted(ctx, a, job); resetErr != nil {
					return resetErr
				}
				job.CreateStarted = false
			}
			return w.retryOrFail(ctx, a, job, "", err)
		}
		if id == "" {
			return w.fail(ctx, a, job, "", "clip creation returned no ID")
		}
		return w.save(ctx, a, job, id, "", w.now().Add(w.PollIntervalOrDefault()))
	}
	if !job.ClipRequestedAt.IsZero() && w.now().Sub(job.ClipRequestedAt) >= readinessWindow {
		return w.complete(ctx, a, job, "unavailable", job.ClipID, "", "clip did not become ready within Twitch processing window")
	}
	pollCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	ready, err := w.Twitch.GetClip(pollCtx, a, job.ClipID)
	cancel()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return w.retryOrFail(ctx, a, job, job.ClipID, err)
	}
	if ready.Ready {
		if ready.URL == "" {
			return w.fail(ctx, a, job, job.ClipID, "ready clip response had no safe URL")
		}
		return w.complete(ctx, a, job, "ready", job.ClipID, ready.URL, "")
	}
	return w.save(ctx, a, job, job.ClipID, "", w.now().Add(w.PollIntervalOrDefault()))
}
func (w *Worker) PollIntervalOrDefault() time.Duration {
	if w.PollInterval <= 0 {
		return 500 * time.Millisecond
	}
	return w.PollInterval
}

func (w *Worker) retryOrFail(ctx context.Context, a auth.ChannelContext, job receipt.ClipJob, clipID string, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var h *twitch.HTTPError
	if errors.As(cause, &h) {
		if h.StatusCode >= 400 && h.StatusCode < 500 && h.StatusCode != http.StatusTooManyRequests {
			return w.fail(ctx, a, job, clipID, safeFailure(cause))
		}
		if h.StatusCode == http.StatusTooManyRequests {
			if job.Attempts >= 2 {
				return w.fail(ctx, a, job, clipID, "Twitch rate limit retry budget exhausted")
			}
			delay := h.RetryAfter
			if delay > 30*time.Second {
				return w.fail(ctx, a, job, clipID, "Twitch rate limit exceeds bounded retry window")
			}
			if delay <= 0 {
				delay = time.Duration(1<<job.Attempts) * time.Second
			}
			return w.save(ctx, a, job, clipID, safeFailure(cause), w.now().Add(delay))
		}
	}
	// A POST transport/5xx error is ambiguous: Twitch may have created a clip
	// even though its response was lost. Never retry that write blindly.
	if clipID == "" {
		return w.fail(ctx, a, job, "", "clip creation result unknown; transcript evidence retained")
	}
	if job.Attempts >= 2 {
		return w.fail(ctx, a, job, clipID, safeFailure(cause))
	}
	delay := time.Duration(1<<job.Attempts) * time.Second
	if delay > 4*time.Second {
		delay = 4 * time.Second
	}
	return w.save(ctx, a, job, clipID, safeFailure(cause), w.now().Add(delay))
}
func safeFailure(err error) string {
	var h *twitch.HTTPError
	if errors.As(err, &h) {
		return fmt.Sprintf("Twitch HTTP %d", h.StatusCode)
	}
	return "Twitch clip request failed"
}
func (w *Worker) fail(ctx context.Context, a auth.ChannelContext, j receipt.ClipJob, id, detail string) error {
	return w.complete(ctx, a, j, "failed", id, "", detail)
}
func (w *Worker) save(ctx context.Context, a auth.ChannelContext, j receipt.ClipJob, id, detail string, next time.Time) error {
	return w.Runtime.WithActive(a, func(ch *channelruntime.Channel) error {
		return ch.Events.CommitError(events.Status, func() (events.OverlayEvent, error) {
			if err := w.Ledger.SaveClipJobAttempt(ctx, authority(a), j, id, detail, next); err != nil {
				return events.OverlayEvent{}, err
			}
			return events.OverlayEvent{OccurredAt: w.now(), Refresh: true}, nil
		})
	})
}
func (w *Worker) complete(ctx context.Context, a auth.ChannelContext, j receipt.ClipJob, status, id, url, detail string) error {
	return w.Runtime.WithActive(a, func(ch *channelruntime.Channel) error {
		return ch.Events.CommitError(events.Status, func() (events.OverlayEvent, error) {
			if err := w.Ledger.CompleteClipJobBound(ctx, authority(a), j, status, id, url, detail); err != nil {
				return events.OverlayEvent{}, err
			}
			return events.OverlayEvent{OccurredAt: w.now(), Refresh: true}, nil
		})
	})
}

func (w *Worker) markCreateStarted(ctx context.Context, a auth.ChannelContext, j receipt.ClipJob) error {
	return w.Runtime.WithActive(a, func(*channelruntime.Channel) error { return w.Ledger.MarkClipCreateStarted(ctx, authority(a), j) })
}
func (w *Worker) resetCreateStarted(ctx context.Context, a auth.ChannelContext, j receipt.ClipJob) error {
	return w.Runtime.WithActive(a, func(*channelruntime.Channel) error { return w.Ledger.ResetClipCreateStarted(ctx, authority(a), j) })
}

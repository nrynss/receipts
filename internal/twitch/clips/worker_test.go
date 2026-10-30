package clips

import (
	"context"
	"errors"
	"testing"
	"time"

	"receipts/internal/auth"
	"receipts/internal/receipt"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
	"receipts/internal/twitch"
	"receipts/internal/twitch/connection"
)

type fakeLedger struct {
	job      receipt.ClipJob
	status   string
	attempts int
	complete string
	saveErr  error
}

func (l *fakeLedger) ClaimDueClipJobs(_ context.Context, a store.DetectionAuthority, _ int, _ time.Duration) ([]receipt.ClipJob, error) {
	if l.status != "" {
		return nil, nil
	}
	j := l.job
	j.LeaseToken = "lease-1"
	j.LeaseGeneration = a.Generation
	return []receipt.ClipJob{j}, nil
}
func (l *fakeLedger) MarkClipCreateStarted(_ context.Context, _ store.DetectionAuthority, j receipt.ClipJob) error {
	l.job.CreateStarted = true
	return nil
}
func (l *fakeLedger) ResetClipCreateStarted(_ context.Context, _ store.DetectionAuthority, j receipt.ClipJob) error {
	l.job.CreateStarted = false
	return nil
}
func (l *fakeLedger) SaveClipJobAttempt(_ context.Context, _ store.DetectionAuthority, j receipt.ClipJob, id, detail string, next time.Time) error {
	if l.saveErr != nil {
		return l.saveErr
	}
	l.attempts++
	l.job.ClipID = id
	l.job.ClipRequestedAt = time.Now().UTC()
	l.job.Attempts++
	l.job.NextAttemptAt = next
	if detail != "" {
		l.job.LastError = detail
	}
	return nil
}
func (l *fakeLedger) CompleteClipJobBound(_ context.Context, _ store.DetectionAuthority, _ receipt.ClipJob, status, _, _, _ string) error {
	l.status = status
	l.complete = status
	return nil
}

type fakeTwitch struct {
	createCalls, pollCalls int
	createID               string
	createErr, errorPoll   error
	readiness              connection.ClipReadiness
	stop                   func()
}

func (f *fakeTwitch) CreateClip(context.Context, auth.ChannelContext) (string, error) {
	f.createCalls++
	return f.createID, f.createErr
}
func (f *fakeTwitch) GetClip(context.Context, auth.ChannelContext, string) (connection.ClipReadiness, error) {
	f.pollCalls++
	if f.stop != nil {
		f.stop()
	}
	return f.readiness, f.errorPoll
}
func runner(t *testing.T, l *fakeLedger, tw *fakeTwitch) (*Worker, auth.ChannelContext) {
	t.Helper()
	a := auth.ChannelContext{AccountID: "owner", ChannelID: "local", LiveSessionID: "session", Generation: 1}
	m := channelruntime.New(channelruntime.Config{})
	if _, e := m.Start(context.Background(), a); e != nil {
		t.Fatal(e)
	}
	return &Worker{Ledger: l, Twitch: tw, Runtime: m, PollInterval: time.Millisecond}, a
}

func TestReadinessResumesPersistedClipIDAndPublishesReady(t *testing.T) {
	l := &fakeLedger{job: receipt.ClipJob{ID: "job", ChannelID: "local", ReceiptID: "receipt", CommandID: "command", Status: "pending"}}
	tw := &fakeTwitch{createID: "fixture-clip", readiness: connection.ClipReadiness{Ready: true, URL: "https://clips.twitch.tv/fixture-clip"}}
	w, a := runner(t, l, tw)
	if err := w.RunOnce(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if tw.createCalls != 1 || l.job.ClipID != "fixture-clip" {
		t.Fatalf("creation not durably staged: calls=%d job=%+v", tw.createCalls, l.job)
	}
	if err := w.RunOnce(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if tw.createCalls != 1 || tw.pollCalls != 1 || l.status != "ready" {
		t.Fatalf("resume outcome: create=%d poll=%d status=%q", tw.createCalls, tw.pollCalls, l.status)
	}
}
func TestAmbiguousCreateFailureIsNotRetried(t *testing.T) {
	l := &fakeLedger{job: receipt.ClipJob{ID: "job", ChannelID: "local", ReceiptID: "receipt", CommandID: "command", Status: "pending"}}
	tw := &fakeTwitch{createErr: errors.New("connection reset")}
	w, a := runner(t, l, tw)
	if err := w.RunOnce(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if tw.createCalls != 1 || l.status != "failed" || l.job.ClipID != "" {
		t.Fatalf("ambiguous POST was retried or misreported: calls=%d status=%q id=%q", tw.createCalls, l.status, l.job.ClipID)
	}
}
func TestRateLimitRetriesRespectRetryAfterAndBoundAttempts(t *testing.T) {
	l := &fakeLedger{job: receipt.ClipJob{ID: "job", ChannelID: "local", ReceiptID: "receipt", CommandID: "command", Status: "pending", ClipID: "clip", ClipRequestedAt: time.Now().UTC()}}
	tw := &fakeTwitch{errorPoll: &twitch.HTTPError{StatusCode: 429, RetryAfter: 3 * time.Second}}
	w, a := runner(t, l, tw)
	if err := w.RunOnce(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if l.attempts != 1 || l.job.NextAttemptAt.Before(time.Now().Add(2*time.Second)) {
		t.Fatalf("Retry-After not honored: attempts=%d next=%s", l.attempts, l.job.NextAttemptAt)
	}
	l.job.Attempts = 2
	if err := w.RunOnce(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if l.status != "failed" {
		t.Fatalf("retry budget did not terminate: %q", l.status)
	}
}

func TestCreateSuccessThenLostPersistenceNeverIssuesDuplicatePost(t *testing.T) {
	l := &fakeLedger{job: receipt.ClipJob{ID: "job", ChannelID: "local", ReceiptID: "receipt", CommandID: "command", Status: "pending"}, saveErr: errors.New("simulated persistence outage")}
	tw := &fakeTwitch{createID: "fixture-clip"}
	w, a := runner(t, l, tw)
	if err := w.RunOnce(context.Background(), a); err == nil {
		t.Fatal("expected persistence failure")
	}
	if tw.createCalls != 1 || !l.job.CreateStarted {
		t.Fatalf("create intent not persisted: posts=%d job=%+v", tw.createCalls, l.job)
	}
	l.saveErr = nil
	_ = w.Runtime.Stop(a)
	a.Generation = 2
	w.Runtime = channelruntime.New(channelruntime.Config{})
	if _, err := w.Runtime.Start(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := w.RunOnce(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if tw.createCalls != 1 || l.status != "failed" {
		t.Fatalf("ambiguous prior POST retried: posts=%d status=%q", tw.createCalls, l.status)
	}
}

func TestDelayedReadyResultCannotCommitAfterRuntimeStops(t *testing.T) {
	l := &fakeLedger{job: receipt.ClipJob{ID: "job", ChannelID: "local", ReceiptID: "receipt", CommandID: "command", Status: "pending", ClipID: "fixture-clip", ClipRequestedAt: time.Now().UTC()}}
	tw := &fakeTwitch{readiness: connection.ClipReadiness{Ready: true, URL: "https://clips.twitch.tv/fixture-clip"}}
	w, a := runner(t, l, tw)
	tw.stop = func() { _ = w.Runtime.Stop(a) }
	if err := w.RunOnce(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if l.status != "" {
		t.Fatalf("stale clip response committed status %q", l.status)
	}
}

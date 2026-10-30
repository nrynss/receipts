package clips

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	"receipts/internal/commands"
	"receipts/internal/ports"
	"receipts/internal/receipt"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
	"receipts/internal/twitch"
	"receipts/internal/twitch/connection"
)

// These tests deliberately use a real migrated SQLite ledger: the worker's
// fake-ledger tests cannot cover the SQL lease and authority fences.
type integrationClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *integrationClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *integrationClock) Add(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

type integrationIDs struct{ n atomic.Int64 }

func (i *integrationIDs) next() (string, error) {
	return fmt.Sprintf("integration-%d", i.n.Add(1)), nil
}

type integrationFixture struct {
	t                *testing.T
	ctx              context.Context
	path             string
	db               *sqlite.DB
	ledger           *store.Store
	clock            *integrationClock
	ids              *integrationIDs
	account, channel string
	session          receipt.Session
	authority        auth.ChannelContext
	runtime          *channelruntime.Manager
	worker           *Worker
}

func newIntegrationFixture(t *testing.T) *integrationFixture {
	t.Helper()
	f := &integrationFixture{t: t, ctx: context.Background(), path: filepath.Join(t.TempDir(), "clips.db"), clock: &integrationClock{now: time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)}, ids: &integrationIDs{}, account: "owner-id", channel: "channel-id"}
	f.open()
	channel, err := f.ledger.CreateChannel(f.ctx, "broadcaster-id", "broadcaster")
	if err != nil {
		t.Fatal(err)
	}
	f.channel = channel.ID
	if _, err := f.db.Writer().Exec(`INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner-id','twitch','broadcaster-id','broadcaster',?,x'01',?,?, 'connected',?)`, f.channel, f.clock.Now().Add(time.Hour).UnixMilli(), "clips:edit", f.clock.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	f.startSession(1)
	f.startRuntime()
	t.Cleanup(func() {
		if f.runtime != nil {
			_ = f.runtime.Stop(f.authority)
		}
		if f.ledger != nil {
			_ = f.ledger.Close()
		}
		if f.db != nil {
			_ = f.db.Close()
		}
	})
	return f
}
func (f *integrationFixture) open() {
	f.t.Helper()
	db, err := sqlite.Open(f.ctx, sqlite.Config{Path: f.path, Now: f.clock.Now})
	if err != nil {
		f.t.Fatal(err)
	}
	ledger, err := store.NewWithIDSource(f.ctx, db, store.Retention{MaxSegments: 100}, f.clock.Now, f.ids.next)
	if err != nil {
		_ = db.Close()
		f.t.Fatal(err)
	}
	f.db, f.ledger = db, ledger
}
func (f *integrationFixture) reopen() {
	f.t.Helper()
	if f.ledger != nil {
		_ = f.ledger.Close()
	}
	if f.db != nil {
		_ = f.db.Close()
	}
	f.open()
}
func (f *integrationFixture) startSession(gen uint64) {
	f.t.Helper()
	s, err := f.ledger.StartSession(f.ctx, f.channel)
	if err != nil {
		f.t.Fatal(err)
	}
	f.session = s
	f.authority = auth.ChannelContext{AccountID: f.account, ChannelID: f.channel, LiveSessionID: s.ID, Generation: gen}
}
func (f *integrationFixture) startRuntime() {
	f.t.Helper()
	f.runtime = channelruntime.New(channelruntime.Config{})
	if _, err := f.runtime.StartVerified(f.ctx, f.authority, f.ledger); err != nil {
		f.t.Fatal(err)
	}
}
func (f *integrationFixture) capture(command, user string) receipt.Receipt {
	f.t.Helper()
	f.clock.Add(3 * time.Second)
	now := f.clock.Now()
	seg := receipt.Segment{ID: "source-" + command, SessionID: f.session.ID, Start: time.Second, End: 2 * time.Second, Text: "I promise to keep this route", SpeakerID: "speaker", Attribution: "broadcaster"}
	if err := f.ledger.AddSegment(f.ctx, seg); err != nil {
		f.t.Fatal(err)
	}
	svc := commands.Service{Ledger: f.ledger, Extractor: fixtureExtractor{seg.ID}, Sensitive: fixtureSensitive{}, Clips: f.worker, Now: f.clock.Now, Lookback: time.Minute}
	result, err := svc.Handle(f.ctx, commands.Authority{ChannelID: f.channel, SessionID: f.session.ID}, ports.ChatCommand{ID: command, UserID: user, Login: user, Command: "!receipt", ReceivedAt: now})
	if err != nil {
		f.t.Fatal(err)
	}
	if result.Code != commands.ReplyCaptured || result.Receipt == nil {
		f.t.Fatalf("capture result: %#v", result)
	}
	return *result.Receipt
}

type fixtureExtractor struct{ source string }

func (e fixtureExtractor) Extract(context.Context, []receipt.Segment) (commands.Extraction, error) {
	return commands.Extraction{Claim: "Keep this route", SourceIDs: []string{e.source}}, nil
}

type fixtureSensitive struct{}

func (fixtureSensitive) Sensitive(context.Context, string) (bool, error) { return false, nil }
func (f *integrationFixture) setWorker(tw Twitch) {
	f.worker = &Worker{Ledger: f.ledger, Twitch: tw, Runtime: f.runtime, Now: f.clock.Now, PollInterval: time.Second}
}

type integrationTwitch struct {
	mu                   sync.Mutex
	creates, polls       int
	id                   string
	createErr, errorPoll error
	ready                connection.ClipReadiness
	entered, release     chan struct{}
}

func (tw *integrationTwitch) CreateClip(ctx context.Context, _ auth.ChannelContext) (string, error) {
	tw.mu.Lock()
	tw.creates++
	entered, release := tw.entered, tw.release
	id, err := tw.id, tw.createErr
	tw.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return id, err
}
func (tw *integrationTwitch) GetClip(context.Context, auth.ChannelContext, string) (connection.ClipReadiness, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.polls++
	return tw.ready, tw.errorPoll
}
func (tw *integrationTwitch) counts() (int, int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.creates, tw.polls
}

func TestSQLiteCaptureCreateRestartAndReadiness(t *testing.T) {
	f := newIntegrationFixture(t)
	tw := &integrationTwitch{id: "fixture-clip", ready: connection.ClipReadiness{Ready: true, URL: "https://clips.twitch.tv/fixture-clip"}}
	f.setWorker(tw)
	r := f.capture("command-one", "viewer-one")
	if err := f.worker.RunOnce(f.ctx, f.authority); err != nil {
		t.Fatal(err)
	}
	job, err := f.ledger.ClipJobForReceipt(f.ctx, f.channel, r.ID)
	if err != nil || job.ClipID != "fixture-clip" || job.Status != "pending" {
		t.Fatalf("created job=%+v err=%v", job, err)
	}
	_ = f.runtime.Stop(f.authority)
	f.runtime = nil
	f.reopen()
	f.clock.Add(2 * time.Second)
	f.authority.Generation = 2
	f.startRuntime()
	f.setWorker(tw)
	if err = f.worker.RunOnce(f.ctx, f.authority); err != nil {
		t.Fatal(err)
	}
	job, err = f.ledger.ClipJobForReceipt(f.ctx, f.channel, r.ID)
	if err != nil || job.Status != "ready" || job.ClipID != "fixture-clip" || job.ClipURL != "https://clips.twitch.tv/fixture-clip" {
		t.Fatalf("ready job=%+v err=%v", job, err)
	}
	if c, p := tw.counts(); c != 1 || p != 1 {
		t.Fatalf("remote calls create=%d poll=%d", c, p)
	}
}

func TestSQLiteConcurrentWorkersLeaseOneCreate(t *testing.T) {
	f := newIntegrationFixture(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	tw := &integrationTwitch{id: "fixture-clip", entered: entered, release: release}
	f.setWorker(tw)
	r := f.capture("command-one", "viewer-one")
	done := make(chan error, 1)
	go func() { done <- f.worker.RunOnce(f.ctx, f.authority) }()
	<-entered
	other := *f.worker
	if err := other.RunOnce(f.ctx, f.authority); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	job, err := f.ledger.ClipJobForReceipt(f.ctx, f.channel, r.ID)
	if err != nil || job.ClipID != "fixture-clip" {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	if c, _ := tw.counts(); c != 1 {
		t.Fatalf("created %d clips", c)
	}
}

func TestSQLiteCreateIntentSurvivesCrashWithoutSecondPost(t *testing.T) {
	f := newIntegrationFixture(t)
	tw := &integrationTwitch{id: "fixture-clip"}
	f.setWorker(tw)
	r := f.capture("command-one", "viewer-one")
	jobs, err := f.ledger.ClaimDueClipJobs(f.ctx, authority(f.authority), 1, 15*time.Second)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim jobs=%d err=%v", len(jobs), err)
	}
	if err = f.ledger.MarkClipCreateStarted(f.ctx, authority(f.authority), jobs[0]); err != nil {
		t.Fatal(err)
	}
	_ = f.runtime.Stop(f.authority)
	f.runtime = nil
	f.reopen()
	f.clock.Add(16 * time.Second)
	f.authority.Generation = 2
	f.startRuntime()
	f.setWorker(tw)
	if err = f.worker.RunOnce(f.ctx, f.authority); err != nil {
		t.Fatal(err)
	}
	job, err := f.ledger.ClipJobForReceipt(f.ctx, f.channel, r.ID)
	if err != nil || job.Status != "failed" || job.ClipID != "" {
		t.Fatalf("recovered job=%+v err=%v", job, err)
	}
	if c, _ := tw.counts(); c != 0 {
		t.Fatalf("recovery duplicated create: %d", c)
	}
}

func TestSQLiteStaleLeaseForeignOwnerRolloverAndVoidCannotCommit(t *testing.T) {
	f := newIntegrationFixture(t)
	tw := &integrationTwitch{ready: connection.ClipReadiness{Ready: true, URL: "https://clips.twitch.tv/fixture-clip"}}
	f.setWorker(tw)
	r := f.capture("command-one", "viewer-one")
	jobs, err := f.ledger.ClaimDueClipJobs(f.ctx, authority(f.authority), 1, time.Second)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim: %v %d", err, len(jobs))
	}
	old := jobs[0]
	f.clock.Add(2 * time.Second)
	fresh, err := f.ledger.ClaimDueClipJobs(f.ctx, authority(f.authority), 1, 15*time.Second)
	if err != nil || len(fresh) != 1 {
		t.Fatalf("reclaim: %v %d", err, len(fresh))
	}
	if err = f.ledger.SaveClipJobAttempt(f.ctx, authority(f.authority), old, "fixture-clip", "", f.clock.Now().Add(time.Second)); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("stale save error=%v", err)
	}
	if err = f.ledger.CompleteClipJobBound(f.ctx, authority(f.authority), old, "ready", "fixture-clip", "https://clips.twitch.tv/fixture-clip", ""); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("stale completion error=%v", err)
	}
	foreign := authority(f.authority)
	foreign.AccountID = "other-owner"
	if err = f.ledger.CompleteClipJobBound(f.ctx, foreign, fresh[0], "ready", "fixture-clip", "https://clips.twitch.tv/fixture-clip", ""); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("foreign completion=%v", err)
	}
	if err = f.ledger.Void(f.ctx, f.channel, r.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.ledger.CompleteClipJobBound(f.ctx, authority(f.authority), fresh[0], "ready", "fixture-clip", "https://clips.twitch.tv/fixture-clip", ""); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("void receipt completion=%v", err)
	}
	job, err := f.ledger.ClipJobForReceipt(f.ctx, f.channel, r.ID)
	if err != nil || job.Status != "pending" {
		t.Fatalf("void job mutated: %+v %v", job, err)
	}
	_ = f.runtime.Stop(f.authority)
	if err = f.ledger.EndSession(f.ctx, f.channel, f.session.ID); err != nil {
		t.Fatal(err)
	}
	f.runtime = nil
	f.startSession(2)
	f.startRuntime()
	f.setWorker(tw)
	r2 := f.capture("command-two", "viewer-two")
	current, err := f.ledger.ClaimDueClipJobs(f.ctx, authority(f.authority), 1, 15*time.Second)
	if err != nil || len(current) != 1 {
		t.Fatalf("new-session claim: jobs=%d err=%v", len(current), err)
	}
	if err = f.ledger.EndSession(f.ctx, f.channel, f.session.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.ledger.CompleteClipJobBound(f.ctx, authority(f.authority), current[0], "ready", "fixture-clip", "https://clips.twitch.tv/fixture-clip", ""); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("rolled-over session completion=%v", err)
	}
	job, err = f.ledger.ClipJobForReceipt(f.ctx, f.channel, r2.ID)
	if err != nil || job.Status != "pending" {
		t.Fatalf("rollover job mutated: %+v %v", job, err)
	}
}

func TestSQLiteRateLimitRetryIsBoundedAndDurable(t *testing.T) {
	f := newIntegrationFixture(t)
	tw := &integrationTwitch{createErr: &twitch.HTTPError{StatusCode: 429, RetryAfter: 4 * time.Second}}
	f.setWorker(tw)
	r := f.capture("command-one", "viewer-one")
	if err := f.worker.RunOnce(f.ctx, f.authority); err != nil {
		t.Fatal(err)
	}
	job, err := f.ledger.ClipJobForReceipt(f.ctx, f.channel, r.ID)
	if err != nil || job.Status != "pending" || job.Attempts != 1 || job.CreateStarted || job.ClipID != "" || !job.NextAttemptAt.Equal(f.clock.Now().Add(4*time.Second)) {
		t.Fatalf("rate limit job=%+v err=%v", job, err)
	}
	_ = f.runtime.Stop(f.authority)
	f.runtime = nil
	f.reopen()
	f.clock.Add(4 * time.Second)
	f.authority.Generation = 2
	f.startRuntime()
	f.setWorker(tw)
	if err = f.worker.RunOnce(f.ctx, f.authority); err != nil {
		t.Fatal(err)
	}
	job, err = f.ledger.ClipJobForReceipt(f.ctx, f.channel, r.ID)
	if err != nil || job.ClipID != "" || job.Status != "pending" || job.Attempts != 2 {
		t.Fatalf("retried job=%+v err=%v", job, err)
	}
	_ = f.runtime.Stop(f.authority)
	f.runtime = nil
	f.reopen()
	f.clock.Add(4 * time.Second)
	f.authority.Generation = 3
	f.startRuntime()
	f.setWorker(tw)
	if err = f.worker.RunOnce(f.ctx, f.authority); err != nil {
		t.Fatal(err)
	}
	job, err = f.ledger.ClipJobForReceipt(f.ctx, f.channel, r.ID)
	if err != nil || job.Status != "failed" {
		t.Fatalf("bounded retries did not terminate durably: job=%+v err=%v", job, err)
	}
	if c, _ := tw.counts(); c != 3 {
		t.Fatalf("429 retry budget allowed %d create calls", c)
	}
}

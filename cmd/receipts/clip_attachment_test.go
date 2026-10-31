package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	"receipts/internal/events"
	"receipts/internal/receipt"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
	"receipts/internal/twitch/clips"
	"receipts/internal/twitch/connection"
	"receipts/internal/twitch/eventsub"
)

const hostedClipID = "offline-clip-1"

type clipFixtureResolver struct{ grant connection.Grant }

func (r clipFixtureResolver) Resolve(context.Context, auth.ChannelContext) (connection.Grant, error) {
	return r.grant, nil
}

type clipFixtureTokens struct{}

func (clipFixtureTokens) AccessToken(context.Context, string) (string, error) {
	return "fixture-token", nil
}
func (clipFixtureTokens) RefreshAfterUnauthorized(context.Context, string, string) (string, error) {
	return "", connection.ErrLost
}

type clipFixtureDialer struct{}

func (clipFixtureDialer) Dial(context.Context, string) (eventsub.Socket, error) {
	return &clipFixtureSocket{welcome: []byte(`{"metadata":{"message_type":"session_welcome"},"payload":{"session":{"id":"fixture-eventsub-session","status":"connected","keepalive_timeout_seconds":30}}}`)}, nil
}

type clipFixtureSocket struct {
	mu      sync.Mutex
	welcome []byte
	closed  chan struct{}
	close   sync.Once
}

func (s *clipFixtureSocket) Next(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	if s.welcome != nil {
		frame := s.welcome
		s.welcome = nil
		s.mu.Unlock()
		return frame, nil
	}
	if s.closed == nil {
		s.closed = make(chan struct{})
	}
	closed := s.closed
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-closed:
		return nil, context.Canceled
	}
}

func (s *clipFixtureSocket) Close() error {
	s.mu.Lock()
	if s.closed == nil {
		s.closed = make(chan struct{})
	}
	closed := s.closed
	s.mu.Unlock()
	s.close.Do(func() { close(closed) })
	return nil
}

func TestProductionAttachmentProcessesOnlyItsQueuedClipJobsAndJoinsOnStop(t *testing.T) {
	ctx := context.Background()
	clock := time.Now().UTC().Truncate(time.Millisecond)
	current := clock.Add(4 * time.Second)
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "clip-attachment.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, func() time.Time { return current })
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()

	manager := channelruntime.New(channelruntime.Config{MaxChannels: 2})
	t.Cleanup(func() { manager.StopAccount("owner-a"); manager.StopAccount("owner-b") })
	makeOwner := func(name, platform string) auth.ChannelContext {
		t.Helper()
		channel, err := ledger.CreateChannel(ctx, platform, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES(?,'twitch',?,?,?,X'00',?,'clips:edit user:read:chat user:write:chat','connected',?)`, "owner-"+name, platform, name, channel.ID, current.Add(time.Hour).UnixMilli(), current.UnixMilli()); err != nil {
			t.Fatal(err)
		}
		activation, err := ledger.StartOwnedLiveSession(ctx, "owner-"+name, channel.ID, "broadcast-"+name, clock, true)
		if err != nil {
			t.Fatal(err)
		}
		return auth.ChannelContext{AccountID: "owner-" + name, ChannelID: channel.ID, LiveSessionID: activation.Session.ID, Generation: activation.Generation}
	}
	a := makeOwner("a", "platform-a")
	b := makeOwner("b", "platform-b")
	chanA, err := manager.StartVerified(ctx, a, ledger)
	if err != nil {
		t.Fatal(err)
	}
	chanB, err := manager.StartVerified(ctx, b, ledger)
	if err != nil {
		t.Fatal(err)
	}
	seedReceipt := func(authority auth.ChannelContext, name string) receipt.Receipt {
		t.Helper()
		segment := receipt.Segment{ID: "source-" + name, SessionID: authority.LiveSessionID, Start: time.Second, End: 2 * time.Second, Text: "I will never take the shortcut again.", Attribution: "broadcaster"}
		if err := ledger.AddSegment(ctx, segment); err != nil {
			t.Fatal(err)
		}
		cmdAt := current.Add(3 * time.Second)
		current = cmdAt.Add(time.Second)
		commandID, userID := "command-"+name, "viewer-"+name
		state, err := ledger.BeginReceiptCommand(ctx, authority.ChannelID, authority.LiveSessionID, commandID, userID, cmdAt)
		if err != nil || state != "processing" {
			t.Fatalf("begin %s fixture command: state=%q err=%v", name, state, err)
		}
		captured, err := ledger.CaptureReceipt(ctx, receipt.CaptureInput{AccountID: authority.AccountID, Generation: authority.Generation, CommandID: commandID, ChannelID: authority.ChannelID, SessionID: authority.LiveSessionID, UserID: userID, Login: "viewer", Claim: segment.Text, ReceivedAt: cmdAt, Window: time.Minute, Evidence: []receipt.EvidenceInput{{SourceID: segment.ID, Text: segment.Text, Start: segment.Start, End: segment.End, Attribution: segment.Attribution}}})
		if err != nil {
			t.Fatalf("capture %s fixture receipt: %v", name, err)
		}
		return captured
	}
	receiptA := seedReceipt(a, "a")
	receiptB := seedReceipt(b, "b")
	jobB, err := ledger.ClipJobForReceipt(ctx, b.ChannelID, receiptB.ID)
	if err != nil || jobB.Status != "pending" {
		t.Fatalf("owner B fixture job=%+v err=%v", jobB, err)
	}

	var creates, polls, subscriptions atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/clips":
			creates.Add(1)
			_, _ = w.Write([]byte(`{"data":[{"id":"` + hostedClipID + `"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/clips":
			polls.Add(1)
			_, _ = w.Write([]byte(`{"data":[{"id":"` + hostedClipID + `","url":"https://clips.twitch.tv/` + hostedClipID + `","broadcaster_id":"platform-a"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/eventsub/subscriptions":
			subscriptions.Add(1)
			_, _ = w.Write([]byte(`{"data":[{"status":"enabled"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	adapter := &connection.Adapter{Runtime: manager, Resolver: clipFixtureResolver{grant: connection.Grant{ClientID: "fixture-client", SubjectID: "platform-a", ChannelPlatformID: "platform-a", Scopes: []string{"clips:edit", "user:read:chat", "user:write:chat"}}}, Tokens: clipFixtureTokens{}, HTTP: api.Client(), BaseURL: api.URL}
	attachment := &intelligenceAttachment{ledger: ledger, runtime: manager, adapter: adapter, apiKey: "never-used", extractionModel: "gpt-6-luna", judgeModel: "gpt-6-luna", embeddingModel: "text-embedding-3-small", embeddingVersion: "claim-v1", embeddingDimensions: 1536, eventSubDialer: clipFixtureDialer{}}
	eventsA := chanA.Events.Broker.Subscribe(ctx, events.TopicOverlay)
	defer eventsA.Cancel()
	cleanupA, err := attachment.attach(chanA.Context, a)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		job, err := ledger.ClipJobForReceipt(ctx, a.ChannelID, receiptA.ID)
		return err == nil && job.Status == "ready" && subscriptions.Load() == 1
	})
	if got := creates.Load(); got != 1 {
		t.Fatalf("clip create calls=%d want 1", got)
	}
	if got := polls.Load(); got != 1 {
		t.Fatalf("clip readiness polls=%d want 1", got)
	}
	if chanA.Events.Sequence() != 2 {
		t.Fatalf("clip status events=%d want save+ready events", chanA.Events.Sequence())
	}
	for sequence := uint64(1); sequence <= 2; sequence++ {
		select {
		case event := <-eventsA.Events:
			payload, ok := event.Data.(events.OverlayEvent)
			if event.Name != events.Status || !ok || payload.Sequence != sequence || !payload.Refresh {
				t.Fatalf("clip event %d: name=%s data=%+v", sequence, event.Name, event.Data)
			}
		case <-time.After(time.Second):
			t.Fatalf("clip status event %d missing", sequence)
		}
	}
	readyA, err := ledger.GetReceipt(ctx, receiptA.ID)
	if err != nil || readyA.ClipStatus != receipt.ClipReady || readyA.ClipURL != "https://clips.twitch.tv/"+hostedClipID {
		t.Fatalf("owner A ready receipt=%+v err=%v", readyA, err)
	}
	if chanB.Events.Sequence() != 0 {
		t.Fatalf("owner B received %d foreign events", chanB.Events.Sequence())
	}

	cleanupDone := make(chan struct{})
	go func() {
		cleanupA()
		close(cleanupDone)
	}()
	select {
	case <-cleanupDone:
	case <-time.After(time.Second):
		t.Fatal("attachment cleanup waited for runtime cancellation instead of stopping its worker")
	}
	if _, err := manager.Resolve(a); err != nil {
		t.Fatalf("attachment cleanup stopped the channel runtime: %v", err)
	}
	lateReceipt := seedReceipt(a, "late")
	time.Sleep(100 * time.Millisecond)
	lateJob, err := ledger.ClipJobForReceipt(ctx, a.ChannelID, lateReceipt.ID)
	if err != nil || lateJob.Status != "pending" || creates.Load() != 1 || chanA.Events.Sequence() != 2 {
		t.Fatalf("clip work continued after cleanup: job=%+v creates=%d sequence=%d err=%v", lateJob, creates.Load(), chanA.Events.Sequence(), err)
	}
	if err := manager.Stop(a); err != nil {
		t.Fatal(err)
	}
	staleWorker := &clips.Worker{Ledger: ledger, Twitch: adapter, Runtime: manager, PollInterval: time.Millisecond}
	if err := staleWorker.RunOnce(ctx, a); err == nil {
		t.Fatal("stale generation ran after stop")
	}
	if creates.Load() != 1 || chanA.Events.Sequence() != 2 {
		t.Fatalf("stale runner changed provider/events: create=%d sequence=%d", creates.Load(), chanA.Events.Sequence())
	}

	restarted, err := ledger.StartOwnedLiveSession(ctx, a.AccountID, a.ChannelID, "broadcast-a", current.Add(time.Second), true)
	if err != nil || restarted.Generation != a.Generation+1 || restarted.Session.ID != a.LiveSessionID {
		t.Fatalf("same-broadcast recovery=%+v err=%v", restarted, err)
	}
	a2 := a
	a2.Generation = restarted.Generation
	chanA2, err := manager.StartVerified(ctx, a2, ledger)
	if err != nil {
		t.Fatal(err)
	}
	eventsA2 := chanA2.Events.Broker.Subscribe(ctx, events.TopicOverlay)
	defer eventsA2.Cancel()
	cleanupA2, err := attachment.attach(chanA2.Context, a2)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		job, err := ledger.ClipJobForReceipt(ctx, a.ChannelID, lateReceipt.ID)
		return err == nil && job.Status == "ready"
	})
	if creates.Load() != 2 || polls.Load() != 2 {
		t.Fatalf("restart repeated the completed clip or missed the later job: creates=%d polls=%d", creates.Load(), polls.Load())
	}
	if chanA2.Events.Sequence() != 2 {
		t.Fatalf("recovered clip status events=%d want save+ready", chanA2.Events.Sequence())
	}
	originalA, err := ledger.ClipJobForReceipt(ctx, a.ChannelID, receiptA.ID)
	if err != nil || originalA.Status != "ready" {
		t.Fatalf("restart changed the original completed clip: job=%+v err=%v", originalA, err)
	}
	jobB, err = ledger.ClipJobForReceipt(ctx, b.ChannelID, receiptB.ID)
	if err != nil || jobB.Status != "pending" {
		t.Fatalf("owner B job was consumed by owner A: job=%+v err=%v", jobB, err)
	}
	if err := manager.Stop(a2); err != nil {
		t.Fatal(err)
	}
	cleanupA2()
	if err := manager.Stop(b); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for acceptance condition")
}

package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"receipts/internal/ports"
	"receipts/internal/receipt"
	"receipts/internal/replay"
	"receipts/internal/store"
)

type extraction struct {
	value Extraction
	err   error
	calls int
}

type extractorFunc struct {
	run   func()
	value Extraction
	err   error
}

func (e *extractorFunc) Extract(context.Context, []receipt.Segment) (Extraction, error) {
	if e.run != nil {
		e.run()
	}
	return e.value, e.err
}

func (e *extraction) Extract(_ context.Context, segs []receipt.Segment) (Extraction, error) {
	e.calls++
	return e.value, e.err
}

type classifier bool

func (c classifier) Sensitive(context.Context, string) (bool, error) { return bool(c), nil }

type classifierFunc func(context.Context, string) (bool, error)

func (c classifierFunc) Sensitive(ctx context.Context, text string) (bool, error) {
	return c(ctx, text)
}

type clipFail struct{}

func (clipFail) Enqueue(context.Context, string, string, string, time.Time) error {
	return errors.New("clip provider unavailable")
}

type clipCounter struct {
	calls int
	err   error
}

func (c *clipCounter) Enqueue(context.Context, string, string, string, time.Time) error {
	c.calls++
	return c.err
}

type harness struct {
	store            *store.Store
	channel, session receipt.Session
	channelID        string
	now              time.Time
	path             string
	storeClock       *time.Time
}

func newHarness(t *testing.T) *harness {
	return newHarnessAt(t, 10*time.Second)
}

func newHarnessAt(t *testing.T, sessionAge time.Duration) *harness {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	storeNow := now.Add(-sessionAge)
	clock := &storeNow
	path := filepath.Join(t.TempDir(), "capture.db")
	s, err := store.Open(context.Background(), store.Config{Path: path, Now: func() time.Time { return *clock }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	c, err := s.CreateChannel(context.Background(), "platform-1", "channel-one")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := s.StartSession(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	*clock = now
	return &harness{store: s, channel: sess, session: sess, channelID: c.ID, now: now, path: path, storeClock: clock}
}
func (h *harness) add(t *testing.T, id, text, attr string, start, end time.Duration) {
	t.Helper()
	if err := h.store.AddSegment(context.Background(), receipt.Segment{ID: id, SessionID: h.session.ID, Start: start, End: end, Text: text, SpeakerID: "speaker", Attribution: attr}); err != nil {
		t.Fatal(err)
	}
}
func (h *harness) svc(e Extractor, sens SensitiveClassifier, clip ClipQueue) *Service {
	return &Service{Ledger: h.store, Extractor: e, Sensitive: sens, Clips: clip, Now: func() time.Time { return h.now }, Lookback: 60 * time.Second}
}
func (h *harness) cmd(id, user string) ports.ChatCommand {
	return ports.ChatCommand{ID: id, ChannelID: "forged-channel", SessionID: "forged-session", UserID: user, Login: "regular", Command: "!receipt", ReceivedAt: h.now}
}

func TestCapturePersistsAttributedExactEvidenceAndSurvivesClipFailure(t *testing.T) {
	h := newHarness(t)
	h.add(t, "claim-1", "I will never take that shortcut again.", "broadcaster", 4*time.Second, 7*time.Second)
	e := &extraction{value: Extraction{Claim: "Never take that shortcut again.", SourceIDs: []string{"claim-1"}}}
	r, err := h.svc(e, classifier(false), clipFail{}).Handle(context.Background(), Authority{ChannelID: h.channelID, SessionID: h.session.ID}, h.cmd("cmd-1", "viewer-7"))
	if err != nil || r.Code != ReplyCaptured {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	if r.Receipt == nil || r.Receipt.Claim != "Never take that shortcut again." || r.Receipt.ClippedBy != "regular" {
		t.Fatalf("receipt=%+v", r.Receipt)
	}
	credit, err := h.store.ReceiptCredit(context.Background(), r.Receipt.ID)
	if err != nil || credit.UserID != "viewer-7" || credit.Login != "regular" {
		t.Fatalf("credit=%+v err=%v", credit, err)
	}
	ev, err := h.store.Evidence(context.Background(), r.Receipt.ID)
	if err != nil || len(ev) != 1 {
		t.Fatalf("evidence=%+v err=%v", ev, err)
	}
	if ev[0].SourceID != "claim-1" || ev[0].Text != "I will never take that shortcut again." || ev[0].Start != 4*time.Second || ev[0].End != 7*time.Second || ev[0].Attribution != "broadcaster" {
		t.Fatalf("evidence=%+v", ev[0])
	}
	job, err := h.store.ClipJobForReceipt(context.Background(), h.channelID, r.Receipt.ID)
	if err != nil || job.Status != "pending" {
		t.Fatalf("clip job=%+v err=%v", job, err)
	}
	if err := h.store.CompleteClipJob(context.Background(), h.channelID, "wrong-command", "failed", "", "", "provider failed"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong command updated clip: %v", err)
	}
	if err := h.store.CompleteClipJob(context.Background(), h.channelID, "cmd-1", "failed", "", "", "provider failed"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.CompleteClipJob(context.Background(), h.channelID, "cmd-1", "failed", "", "", "provider failed"); err != nil {
		t.Fatalf("identical clip retry: %v", err)
	}
	job, err = h.store.ClipJobForReceipt(context.Background(), h.channelID, r.Receipt.ID)
	if err != nil || job.Status != "failed" {
		t.Fatalf("clip job=%+v err=%v", job, err)
	}
	stillThere, err := h.store.GetReceipt(context.Background(), r.Receipt.ID)
	if err != nil || stillThere.Claim != r.Receipt.Claim {
		t.Fatalf("failed clip removed receipt: %+v err=%v", stillThere, err)
	}
}

func TestDuplicateConcurrentCommandsProduceOneReceiptAndOneExtraction(t *testing.T) {
	h := newHarness(t)
	h.add(t, "claim-1", "I will never take that shortcut again.", "broadcaster", 0, 2*time.Second)
	e := &extraction{value: Extraction{Claim: "Never take that shortcut again.", SourceIDs: []string{"claim-1"}}}
	svc := h.svc(e, classifier(false), nil)
	auth := Authority{ChannelID: h.channelID, SessionID: h.session.ID}
	cmd := h.cmd("same-id", "viewer-1")
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := []ReplyCode{}
	errs := []error{}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := svc.Handle(context.Background(), auth, cmd)
			mu.Lock()
			defer mu.Unlock()
			codes = append(codes, r.Code)
			errs = append(errs, err)
		}()
	}
	wg.Wait()
	captured := 0
	for i, code := range codes {
		if errs[i] != nil {
			t.Fatalf("err=%v", errs[i])
		}
		if code == ReplyCaptured {
			captured++
		}
	}
	if captured != 1 || e.calls != 1 {
		t.Fatalf("captured=%d extraction calls=%d codes=%v", captured, e.calls, codes)
	}
	rows, err := h.store.PublicReceipts(context.Background(), h.channelID, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
}

func TestGuestSensitiveMissingSpeechAndUncertainEvidenceAreSuppressed(t *testing.T) {
	t.Run("guest", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "guest", "promise by guest", "uncertain", 0, 2*time.Second)
		e := &extraction{}
		r, _ := h.svc(e, classifier(false), nil).Handle(context.Background(), Authority{h.channelID, h.session.ID}, h.cmd("c1", "v1"))
		if r.Code != ReplyNoClaim || e.calls != 0 {
			t.Fatalf("result=%+v calls=%d", r, e.calls)
		}
	})
	t.Run("no speech", func(t *testing.T) {
		h := newHarness(t)
		e := &extraction{}
		r, _ := h.svc(e, classifier(false), nil).Handle(context.Background(), Authority{h.channelID, h.session.ID}, h.cmd("c1", "v1"))
		if r.Code != ReplyNoClaim {
			t.Fatalf("result=%+v", r)
		}
	})
	t.Run("sensitive", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "claim", "I promise a health thing", "broadcaster", 0, 2*time.Second)
		e := &extraction{value: Extraction{Claim: "private health claim", SourceIDs: []string{"claim"}}}
		r, _ := h.svc(e, classifier(true), nil).Handle(context.Background(), Authority{h.channelID, h.session.ID}, h.cmd("c1", "v1"))
		if r.Code != ReplySuppressed || r.Receipt != nil {
			t.Fatalf("result=%+v", r)
		}
		rows, _ := h.store.PublicReceipts(context.Background(), h.channelID, 10)
		if len(rows) != 0 {
			t.Fatalf("sensitive claim persisted: %+v", rows)
		}
	})
	t.Run("out of window source", func(t *testing.T) {
		h := newHarnessAt(t, 50*time.Second)
		h.add(t, "claim", "old promise", "broadcaster", 0, 2*time.Second)
		h.add(t, "recent", "ordinary words", "broadcaster", 40*time.Second, 41*time.Second)
		e := &extraction{value: Extraction{Claim: "old promise", SourceIDs: []string{"claim"}}}
		svc := h.svc(e, classifier(false), nil)
		svc.Lookback = 20 * time.Second
		r, _ := svc.Handle(context.Background(), Authority{h.channelID, h.session.ID}, h.cmd("c1", "v1"))
		if r.Code != ReplyNoClaim {
			t.Fatalf("result=%+v", r)
		}
	})
}

func TestChannelUserThrottleAndStaleSessionFailClosed(t *testing.T) {
	h := newHarness(t)
	h.add(t, "claim", "promise", "broadcaster", 0, 2*time.Second)
	e := &extraction{value: Extraction{Claim: "promise", SourceIDs: []string{"claim"}}}
	svc := h.svc(e, classifier(false), nil)
	auth := Authority{h.channelID, h.session.ID}
	if r, err := svc.Handle(context.Background(), auth, h.cmd("c1", "v1")); err != nil || r.Code != ReplyCaptured {
		t.Fatalf("first=%+v err=%v", r, err)
	}
	if r, err := svc.Handle(context.Background(), auth, h.cmd("c2", "v1")); err != nil || r.Code != ReplyThrottled {
		t.Fatalf("throttled=%+v err=%v", r, err)
	}
	other, err := h.store.StartSession(context.Background(), h.channelID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Handle(context.Background(), Authority{h.channelID, h.session.ID}, h.cmd("c3", "v2")); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale session error=%v", err)
	}
	_ = other
}

func TestCommandDedupSurvivesStoreRestart(t *testing.T) {
	h := newHarness(t)
	h.add(t, "source", "I will take that shortcut again.", "broadcaster", time.Second, 3*time.Second)
	e := &extraction{value: Extraction{Claim: "Will take that shortcut again.", SourceIDs: []string{"source"}}}
	svc := h.svc(e, classifier(false), nil)
	cmd := h.cmd("durable-command", "viewer")
	auth := Authority{h.channelID, h.session.ID}
	first, err := svc.Handle(context.Background(), auth, cmd)
	if err != nil || first.Code != ReplyCaptured {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if err := h.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(context.Background(), store.Config{Path: h.path, Now: func() time.Time { return h.now }})
	if err != nil {
		t.Fatal(err)
	}
	h.store = reopened
	t.Cleanup(func() { _ = reopened.Close() })
	svc.Ledger = reopened
	second, err := svc.Handle(context.Background(), auth, cmd)
	if err != nil || second.Code != ReplyDuplicate || e.calls != 1 {
		t.Fatalf("second=%+v calls=%d err=%v", second, e.calls, err)
	}
}

func TestThrottleCountsNoClaimAndExtractionFailureButNotAcrossChannels(t *testing.T) {
	h := newHarness(t)
	service := h.svc(&extraction{}, classifier(false), nil)
	authA := Authority{h.channelID, h.session.ID}
	first, err := service.Handle(context.Background(), authA, h.cmd("empty-1", "same-viewer"))
	if err != nil || first.Code != ReplyNoClaim {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := service.Handle(context.Background(), authA, h.cmd("empty-2", "same-viewer"))
	if err != nil || second.Code != ReplyThrottled {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	*h.storeClock = h.now.Add(-10 * time.Second)
	other, err := h.store.CreateChannel(context.Background(), "platform-2", "channel-two")
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := h.store.StartSession(context.Background(), other.ID)
	if err != nil {
		t.Fatal(err)
	}
	*h.storeClock = h.now
	authB := Authority{other.ID, otherSession.ID}
	third, err := service.Handle(context.Background(), authB, h.cmd("empty-3", "same-viewer"))
	if err != nil || third.Code != ReplyNoClaim {
		t.Fatalf("other channel was throttled: %+v err=%v", third, err)
	}
}

func TestChannelThrottleCountsOnlyAdmittedAttempts(t *testing.T) {
	h := newHarness(t)
	svc := h.svc(&extraction{}, classifier(false), nil)
	auth := Authority{h.channelID, h.session.ID}
	for i := 0; i < 3; i++ {
		r, err := svc.Handle(context.Background(), auth, h.cmd(fmt.Sprintf("empty-%d", i), fmt.Sprintf("viewer-%d", i)))
		if err != nil || r.Code != ReplyNoClaim {
			t.Fatalf("attempt %d = %+v err=%v", i, r, err)
		}
	}
	blocked, err := svc.Handle(context.Background(), auth, h.cmd("blocked", "viewer-blocked"))
	if err != nil || blocked.Code != ReplyThrottled {
		t.Fatalf("blocked=%+v err=%v", blocked, err)
	}
	// A throttled delivery does not extend the admitted-attempt window.
	h.now = h.now.Add(6 * time.Second)
	*h.storeClock = h.now
	allowed, err := svc.Handle(context.Background(), auth, h.cmd("after-window", "viewer-after"))
	if err != nil || allowed.Code != ReplyNoClaim {
		t.Fatalf("after window=%+v err=%v", allowed, err)
	}
}

func TestExtractionFailureConsumesThrottleBudget(t *testing.T) {
	h := newHarness(t)
	h.add(t, "source", "claim", "broadcaster", time.Second, 2*time.Second)
	failed := h.svc(&extraction{err: errors.New("extractor failed")}, classifier(false), nil)
	first, err := failed.Handle(context.Background(), Authority{h.channelID, h.session.ID}, h.cmd("failed-1", "viewer"))
	if !errors.Is(err, ErrUnavailable) || first.Code != ReplyTranscriptUnavailable {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	goodExtractor := &extraction{value: Extraction{Claim: "claim", SourceIDs: []string{"source"}}}
	second, err := h.svc(goodExtractor, classifier(false), nil).Handle(context.Background(), Authority{h.channelID, h.session.ID}, h.cmd("failed-2", "viewer"))
	if err != nil || second.Code != ReplyThrottled || goodExtractor.calls != 0 {
		t.Fatalf("second=%+v calls=%d err=%v", second, goodExtractor.calls, err)
	}
}

func TestDelayedExtractionCannotCaptureAfterSessionRollover(t *testing.T) {
	h := newHarness(t)
	h.add(t, "source", "claim", "broadcaster", time.Second, 2*time.Second)
	e := &extractorFunc{run: func() {
		if _, err := h.store.StartSession(context.Background(), h.channelID); err != nil {
			t.Error(err)
		}
	}, value: Extraction{Claim: "claim", SourceIDs: []string{"source"}}}
	r, err := h.svc(e, classifier(false), nil).Handle(context.Background(), Authority{h.channelID, h.session.ID}, h.cmd("rollover", "viewer"))
	if !errors.Is(err, store.ErrConflict) || r.Receipt != nil {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	rows, err := h.store.PublicReceipts(context.Background(), h.channelID, 10)
	if err != nil || len(rows) != 0 {
		t.Fatalf("receipts=%+v err=%v", rows, err)
	}
}

func TestCommitUsesFreshStoreClockAfterDelayedExtraction(t *testing.T) {
	h := newHarness(t)
	h.add(t, "source", "claim", "broadcaster", time.Second, 2*time.Second)
	queue := &clipCounter{err: errors.New("provider unavailable")}
	e := &extractorFunc{run: func() { *h.storeClock = h.now.Add(31 * time.Second) }, value: Extraction{Claim: "claim", SourceIDs: []string{"source"}}}
	r, err := h.svc(e, classifier(false), queue).Handle(context.Background(), Authority{h.channelID, h.session.ID}, h.cmd("delayed-commit", "viewer"))
	if !errors.Is(err, store.ErrNotEligible) || r.Receipt != nil || queue.calls != 0 {
		t.Fatalf("result=%+v clip calls=%d err=%v", r, queue.calls, err)
	}
	rows, _ := h.store.PublicReceipts(context.Background(), h.channelID, 10)
	if len(rows) != 0 {
		t.Fatalf("stale capture persisted: %+v", rows)
	}
}

func TestCaptureRejectsChangedPersistedCommandTime(t *testing.T) {
	h := newHarness(t)
	h.add(t, "source", "claim", "broadcaster", time.Second, 2*time.Second)
	cmd := h.cmd("changed-time", "viewer")
	state, err := h.store.BeginReceiptCommand(context.Background(), h.channelID, h.session.ID, cmd.ID, cmd.UserID, cmd.ReceivedAt)
	if err != nil || state != "processing" {
		t.Fatalf("state=%q err=%v", state, err)
	}
	_, err = h.store.CaptureReceipt(context.Background(), receipt.CaptureInput{CommandID: cmd.ID, ChannelID: h.channelID, SessionID: h.session.ID, UserID: cmd.UserID, Login: cmd.Login, Claim: "claim", ReceivedAt: cmd.ReceivedAt.Add(-time.Second), Window: 60 * time.Second, Evidence: []receipt.EvidenceInput{{SourceID: "source", Text: "claim", Start: time.Second, End: 2 * time.Second, Attribution: "broadcaster"}}})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("mismatched receive time error=%v", err)
	}
	newSession, err := h.store.StartSession(context.Background(), h.channelID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.store.CaptureReceipt(context.Background(), receipt.CaptureInput{CommandID: cmd.ID, ChannelID: h.channelID, SessionID: newSession.ID, UserID: cmd.UserID, Login: cmd.Login, Claim: "claim", ReceivedAt: cmd.ReceivedAt, Window: 60 * time.Second, Evidence: []receipt.EvidenceInput{{SourceID: "source", Text: "claim", Start: time.Second, End: 2 * time.Second, Attribution: "broadcaster"}}})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("command rebound to later session: %v", err)
	}
	rows, _ := h.store.PublicReceipts(context.Background(), h.channelID, 10)
	if len(rows) != 0 {
		t.Fatalf("mismatched time persisted: %+v", rows)
	}
}

func TestFreshnessWindowBoundariesAndFutureEvidence(t *testing.T) {
	t.Run("inclusive lookback and maximum age", func(t *testing.T) {
		h := newHarnessAt(t, 60*time.Second)
		h.add(t, "boundary", "boundary statement", "broadcaster", 10*time.Second, 30*time.Second)
		e := &extraction{value: Extraction{Claim: "Boundary statement", SourceIDs: []string{"boundary"}}}
		svc := h.svc(e, classifier(false), nil)
		svc.Lookback = 20 * time.Second
		cmd := h.cmd("boundary-command", "viewer")
		cmd.ReceivedAt = h.now.Add(-30 * time.Second)
		r, err := svc.Handle(context.Background(), Authority{h.channelID, h.session.ID}, cmd)
		if err != nil || r.Code != ReplyCaptured {
			t.Fatalf("boundary=%+v err=%v", r, err)
		}
	})
	t.Run("stale and future chat rejected", func(t *testing.T) {
		h := newHarnessAt(t, 60*time.Second)
		h.add(t, "source", "statement", "broadcaster", 40*time.Second, 41*time.Second)
		e := &extraction{value: Extraction{Claim: "statement", SourceIDs: []string{"source"}}}
		svc := h.svc(e, classifier(false), nil)
		auth := Authority{h.channelID, h.session.ID}
		stale := h.cmd("stale", "viewer")
		stale.ReceivedAt = h.now.Add(-31 * time.Second)
		got, err := svc.Handle(context.Background(), auth, stale)
		if err != nil || got.Code != ReplyNoClaim || e.calls != 0 {
			t.Fatalf("stale=%+v calls=%d err=%v", got, e.calls, err)
		}
		future := h.cmd("future", "viewer-2")
		future.ReceivedAt = h.now.Add(time.Second)
		got, err = svc.Handle(context.Background(), auth, future)
		if err != nil || got.Code != ReplyNoClaim || e.calls != 0 {
			t.Fatalf("future=%+v calls=%d err=%v", got, e.calls, err)
		}
	})
	t.Run("future transcript segment excluded", func(t *testing.T) {
		h := newHarnessAt(t, 60*time.Second)
		h.add(t, "future-source", "future words", "broadcaster", 31*time.Second, 32*time.Second)
		e := &extraction{value: Extraction{Claim: "future words", SourceIDs: []string{"future-source"}}}
		svc := h.svc(e, classifier(false), nil)
		svc.Lookback = 20 * time.Second
		cmd := h.cmd("future-segment", "viewer")
		cmd.ReceivedAt = h.now.Add(-30 * time.Second)
		got, err := svc.Handle(context.Background(), Authority{h.channelID, h.session.ID}, cmd)
		if err != nil || got.Code != ReplyNoClaim {
			t.Fatalf("future segment=%+v err=%v", got, err)
		}
	})
}

func TestSensitiveClassifierSeesOriginalEvidenceAndFailsClosed(t *testing.T) {
	h := newHarness(t)
	h.add(t, "source", "original private financial hardship", "broadcaster", time.Second, 2*time.Second)
	e := &extraction{value: Extraction{Claim: "I will reduce my playtime", SourceIDs: []string{"source"}}}
	var seen []string
	sensitive := classifierFunc(func(_ context.Context, text string) (bool, error) {
		seen = append(seen, text)
		return strings.Contains(text, "financial"), nil
	})
	r, err := h.svc(e, sensitive, nil).Handle(context.Background(), Authority{h.channelID, h.session.ID}, h.cmd("sensitive-source", "viewer"))
	if err != nil || r.Code != ReplySuppressed || len(seen) != 2 || seen[0] != e.value.Claim || seen[1] != "original private financial hardship" {
		t.Fatalf("result=%+v seen=%q err=%v", r, seen, err)
	}
	rows, _ := h.store.PublicReceipts(context.Background(), h.channelID, 10)
	if len(rows) != 0 {
		t.Fatalf("sensitive evidence persisted: %+v", rows)
	}
	h2 := newHarness(t)
	h2.add(t, "source", "original evidence", "broadcaster", time.Second, 2*time.Second)
	classifierErr := classifierFunc(func(context.Context, string) (bool, error) { return false, errors.New("classifier unavailable") })
	got, err := h2.svc(&extraction{value: Extraction{Claim: "safe looking", SourceIDs: []string{"source"}}}, classifierErr, nil).Handle(context.Background(), Authority{h2.channelID, h2.session.ID}, h2.cmd("classifier-error", "viewer"))
	if !errors.Is(err, ErrUnavailable) || got.Code != ReplyTranscriptUnavailable {
		t.Fatalf("classifier error result=%+v err=%v", got, err)
	}
	rows, _ = h2.store.PublicReceipts(context.Background(), h2.channelID, 10)
	if len(rows) != 0 {
		t.Fatalf("classifier error persisted receipt: %+v", rows)
	}
}

func TestRecordedReplayUsesReceiptServiceAndRealLedger(t *testing.T) {
	file, err := os.Open("../../testdata/replay/t21-capture.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := replay.Decode(file)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	clock := replay.NewClock(fixture.Inputs[0].At.Add(-time.Second))
	ledger, err := replay.OpenLedger(context.Background(), filepath.Join(t.TempDir(), "replay.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	e := &extraction{value: Extraction{Claim: "Never take that shortcut again.", SourceIDs: []string{"segment-receipt-1"}}}
	svc := &Service{Ledger: ledger.Store, Extractor: e, Sensitive: classifier(false), Now: clock.Now, Lookback: 20 * time.Second}
	var got Result
	err = fixture.Run(context.Background(), replay.Sinks{Transcript: replay.TranscriptSink{Ledger: ledger}, Chat: ChatSink{Service: svc, Authority: Authority{ChannelID: ledger.Channel.ID, SessionID: ledger.Session.ID}, OnResult: func(r Result) { got = r }}}, clock)
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != ReplyCaptured || got.Receipt == nil || got.Receipt.Claim != "Never take that shortcut again." {
		t.Fatalf("result=%+v", got)
	}
	evidence, err := ledger.Store.Evidence(context.Background(), got.Receipt.ID)
	if err != nil || len(evidence) != 1 || evidence[0].Start != 4*time.Second || evidence[0].End != 7*time.Second {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
}

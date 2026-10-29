package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func (e *extraction) Extract(_ context.Context, segs []receipt.Segment) (Extraction, error) {
	e.calls++
	return e.value, e.err
}

type classifier bool

func (c classifier) Sensitive(context.Context, string) (bool, error) { return bool(c), nil }

type clipFail struct{}

func (clipFail) Enqueue(context.Context, string, string, time.Time) error {
	return errors.New("clip provider unavailable")
}

type harness struct {
	store            *store.Store
	channel, session receipt.Session
	channelID        string
	now              time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s, err := store.Open(context.Background(), store.Config{Path: filepath.Join(t.TempDir(), "capture.db"), Now: func() time.Time { return now }})
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
	return &harness{store: s, channel: sess, session: sess, channelID: c.ID, now: now}
}
func (h *harness) add(t *testing.T, id, text, attr string, start, end time.Duration) {
	t.Helper()
	if err := h.store.AddSegment(context.Background(), receipt.Segment{ID: id, SessionID: h.session.ID, Start: start, End: end, Text: text, SpeakerID: "speaker", Attribution: attr}); err != nil {
		t.Fatal(err)
	}
}
func (h *harness) svc(e Extractor, sens SensitiveClassifier, clip ClipQueue) *Service {
	return &Service{Ledger: h.store, Extractor: e, Sensitive: sens, Clips: clip, Now: func() time.Time { return h.now }, Lookback: 20 * time.Second}
}
func (h *harness) cmd(id, user string) ports.ChatCommand {
	return ports.ChatCommand{ID: id, ChannelID: "forged-channel", SessionID: "forged-session", UserID: user, Login: "regular", Command: "!receipt", ReceivedAt: h.now}
}

func TestCapturePersistsAttributedExactEvidenceAndSurvivesClipFailure(t *testing.T) {
	h := newHarness(t)
	h.add(t, "claim-1", "I will never pick Vex again.", "broadcaster", 4*time.Second, 7*time.Second)
	e := &extraction{value: Extraction{Claim: "Never pick Vex again.", SourceIDs: []string{"claim-1"}}}
	r, err := h.svc(e, classifier(false), clipFail{}).Handle(context.Background(), Authority{ChannelID: h.channelID, SessionID: h.session.ID}, h.cmd("cmd-1", "viewer-7"))
	if err != nil || r.Code != ReplyCaptured {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	if r.Receipt == nil || r.Receipt.Claim != "Never pick Vex again." || r.Receipt.ClippedBy != "regular" {
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
	if ev[0].SourceID != "claim-1" || ev[0].Text != "I will never pick Vex again." || ev[0].Start != 4*time.Second || ev[0].End != 7*time.Second || ev[0].Attribution != "broadcaster" {
		t.Fatalf("evidence=%+v", ev[0])
	}
}

func TestDuplicateConcurrentCommandsProduceOneReceiptAndOneExtraction(t *testing.T) {
	h := newHarness(t)
	h.add(t, "claim-1", "I will never pick Vex again.", "broadcaster", 0, 2*time.Second)
	e := &extraction{value: Extraction{Claim: "Never pick Vex again.", SourceIDs: []string{"claim-1"}}}
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
		h := newHarness(t)
		h.add(t, "claim", "old promise", "broadcaster", 0, 2*time.Second)
		h.add(t, "recent", "ordinary words", "broadcaster", 40*time.Second, 41*time.Second)
		e := &extraction{value: Extraction{Claim: "old promise", SourceIDs: []string{"claim"}}}
		r, _ := h.svc(e, classifier(false), nil).Handle(context.Background(), Authority{h.channelID, h.session.ID}, h.cmd("c1", "v1"))
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
	e := &extraction{value: Extraction{Claim: "Never pick Vex again.", SourceIDs: []string{"segment-receipt-1"}}}
	svc := &Service{Ledger: ledger.Store, Extractor: e, Sensitive: classifier(false), Now: clock.Now, Lookback: 20 * time.Second}
	var got Result
	err = fixture.Run(context.Background(), replay.Sinks{Transcript: replay.TranscriptSink{Ledger: ledger}, Chat: ChatSink{Service: svc, Authority: Authority{ChannelID: ledger.Channel.ID, SessionID: ledger.Session.ID}, OnResult: func(r Result) { got = r }}}, clock)
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != ReplyCaptured || got.Receipt == nil || got.Receipt.Claim != "Never pick Vex again." {
		t.Fatalf("result=%+v", got)
	}
	evidence, err := ledger.Store.Evidence(context.Background(), got.Receipt.ID)
	if err != nil || len(evidence) != 1 || evidence[0].Start != 4*time.Second || evidence[0].End != 7*time.Second {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
}

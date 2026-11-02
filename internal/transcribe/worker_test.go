package transcribe

import (
	"context"
	"encoding/binary"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/sqlite"
	"receipts/internal/audio"
	"receipts/internal/auth"
	"receipts/internal/receipt"
	runtime "receipts/internal/runtime"
)

type fakeTranscriber func(context.Context, []byte) (string, error)

func (f fakeTranscriber) Transcribe(ctx context.Context, b []byte) (string, error) { return f(ctx, b) }

type fakeMeter struct {
	calls  int
	active bool
	charge cost.Price
}

func (m *fakeMeter) Spend(ctx context.Context, _ auth.ChannelContext, _, _ string, p cost.Price, work cost.Work) error {
	m.calls++
	m.charge = p
	_, err := work(ctx)
	return err
}
func (m *fakeMeter) WithActive(_ auth.ChannelContext, fn func(*runtime.Channel) error) error {
	if !m.active {
		return errors.New("stale")
	}
	return fn(nil)
}

type fakeLedger struct {
	segments []receipt.Segment
	err      error
}

func (l *fakeLedger) AddLiveSegment(_ context.Context, _, _, _ string, _ uint64, s receipt.Segment) error {
	if l.err != nil {
		return l.err
	}
	l.segments = append(l.segments, s)
	return nil
}
func TestWorkerOrderingAndAttribution(t *testing.T) {
	m := &fakeMeter{active: true}
	l := &fakeLedger{}
	w := Worker{Authority: auth.ChannelContext{ChannelID: "one", LiveSessionID: "s", Generation: 1}, Meter: m, Ledger: l, NowOffset: func() time.Duration { return time.Hour }, Transcriber: fakeTranscriber(func(context.Context, []byte) (string, error) { return "same phrase", nil })}
	c := Chunk{Sequence: 1, Start: time.Second, End: 2 * time.Second, WAV: testWAV()}
	if err := w.Submit(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if l.segments[0].Attribution != "uncertain" {
		t.Fatal("mixed source attributed")
	}
	if err := w.Submit(context.Background(), c); err == nil {
		t.Fatal("duplicate accepted")
	}
	c.Sequence = 2
	c.Start = 1500 * time.Millisecond
	c.End = 2500 * time.Millisecond
	if err := w.Submit(context.Background(), c); err == nil {
		t.Fatal("overlap accepted")
	}
	c.Start = 2 * time.Second
	c.End = 3 * time.Second
	if err := w.Submit(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if len(l.segments) != 2 || m.calls != 2 || m.charge != 4_500_000 {
		t.Fatal("repeated speech or charge lost")
	}
}
func TestWorkerLateResultAndAttemptCharge(t *testing.T) {
	m := &fakeMeter{active: true}
	l := &fakeLedger{}
	w := Worker{Authority: auth.ChannelContext{ChannelID: "one", LiveSessionID: "s", Generation: 1}, Meter: m, Ledger: l, SoleBroadcaster: true, NowOffset: func() time.Duration { return time.Hour }, Transcriber: fakeTranscriber(func(ctx context.Context, _ []byte) (string, error) { m.active = false; return "late", nil })}
	c := Chunk{Sequence: 1, Start: time.Second, End: 2 * time.Second, WAV: testWAV()}
	if err := w.Submit(context.Background(), c); err == nil || len(l.segments) != 0 {
		t.Fatal("late result persisted")
	}
	m.active = true
	c.Sequence = 2
	c.Start = 2 * time.Second
	c.End = 3 * time.Second
	w.Transcriber = fakeTranscriber(func(context.Context, []byte) (string, error) { return "", errors.New("network") })
	if err := w.Submit(context.Background(), c); err == nil || m.calls != 2 {
		t.Fatal("failed attempt was not metered")
	}
}

func TestWorkerDurableBudgetsAcrossOwners(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "budget.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	budgets, err := runtime.OpenBudgets(ctx, db, runtime.BudgetConfig{GlobalLimit: 9_000_000, ChannelLimit: 4_500_000})
	if err != nil {
		t.Fatal(err)
	}
	manager := runtime.New(runtime.Config{MaxChannels: 2})
	manager.SetBudgets(budgets)
	a := auth.ChannelContext{AccountID: "a", ChannelID: "a", LiveSessionID: "sa", Generation: 1}
	b := auth.ChannelContext{AccountID: "b", ChannelID: "b", LiveSessionID: "sb", Generation: 1}
	if _, err = manager.Start(ctx, a); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(a)
	if _, err = manager.Start(ctx, b); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(b)
	la, lb := &fakeLedger{}, &fakeLedger{}
	transcriber := fakeTranscriber(func(context.Context, []byte) (string, error) { return "words", nil })
	wa := Worker{Authority: a, Meter: manager, Ledger: la, NowOffset: func() time.Duration { return time.Hour }, Transcriber: transcriber}
	wb := Worker{Authority: b, Meter: manager, Ledger: lb, NowOffset: func() time.Duration { return time.Hour }, Transcriber: transcriber}
	c := Chunk{Sequence: 1, Start: time.Second, End: 2 * time.Second, WAV: testWAV()}
	if err = wa.Submit(ctx, c); err != nil {
		t.Fatal(err)
	}
	c.Sequence++
	c.Start = c.End
	c.End += time.Second
	if err = wa.Submit(ctx, c); !errors.Is(err, cost.ErrOverBudget) {
		t.Fatalf("owner cap: %v", err)
	}
	if err = wb.Submit(ctx, c); err != nil {
		t.Fatal(err)
	}
	if len(la.segments) != 1 || len(lb.segments) != 1 || la.segments[0].SessionID != "sa" || lb.segments[0].SessionID != "sb" {
		t.Fatal("cross-owner evidence")
	}
	_, remaining, err := budgets.Remaining(auth.WithChannelContext(ctx, a), "a")
	if err != nil || remaining != 0 {
		t.Fatalf("global remaining %d: %v", remaining, err)
	}
}

func testWAV() []byte {
	b := make([]byte, 44+32000)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], 32000)
	return b
}

func TestWorkerRejectsUnverifiedDurationAndFuture(t *testing.T) {
	m := &fakeMeter{active: true}
	w := Worker{Authority: auth.ChannelContext{AccountID: "a", ChannelID: "a", LiveSessionID: "s", Generation: 1}, Meter: m, Ledger: &fakeLedger{}, NowOffset: func() time.Duration { return 2 * time.Second }, Transcriber: fakeTranscriber(func(context.Context, []byte) (string, error) { return "", nil })}
	c := Chunk{Sequence: 1, Start: time.Second, End: 3 * time.Second, WAV: testWAV()}
	if err := w.Submit(context.Background(), c); err == nil {
		t.Fatal("duration/future accepted")
	}
	c.End = 2 * time.Second
	c.WAV[24] = 0
	if err := w.Submit(context.Background(), c); err == nil {
		t.Fatal("invalid sample rate accepted")
	}
	if m.calls != 0 {
		t.Fatal("invalid audio reached provider")
	}
}

func (l *fakeLedger) ClaimAudioChunk(context.Context, string, string, string, uint64, time.Duration, time.Duration) error {
	return nil
}

func TestWorkerNotifiesOnlyPersistedBroadcasterSegments(t *testing.T) {
	m := &fakeMeter{active: true}
	l := &fakeLedger{}
	var notified []receipt.Segment
	w := Worker{Authority: auth.ChannelContext{ChannelID: "one", LiveSessionID: "s", Generation: 1}, Meter: m, Ledger: l, SoleBroadcaster: true, NowOffset: func() time.Duration { return time.Hour }, Transcriber: fakeTranscriber(func(context.Context, []byte) (string, error) { return "speech", nil }), OnSegment: func(s receipt.Segment) {
		if len(l.segments) != 1 || l.segments[0] != s {
			t.Fatal("callback before persistence")
		}
		notified = append(notified, s)
	}}
	c := Chunk{Sequence: 1, Start: time.Second, End: 2 * time.Second, WAV: testWAV()}
	if err := w.Submit(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if len(notified) != 1 || len(l.segments) != 1 || notified[0].ID == "" || notified[0] != l.segments[0] {
		t.Fatalf("persisted=%+v notified=%+v", l.segments, notified)
	}
	l.err = errors.New("write failed")
	c.Sequence++
	c.Start = c.End
	c.End += time.Second
	if err := w.Submit(context.Background(), c); err == nil {
		t.Fatal("failed persistence succeeded")
	}
	if len(notified) != 1 {
		t.Fatal("notified failed persistence")
	}
	l.err = nil
	w.SoleBroadcaster = false
	c.Sequence++
	c.Start = c.End
	c.End += time.Second
	if err := w.Submit(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if len(notified) != 1 {
		t.Fatal("notified uncertain speech")
	}
}

type windowLedger struct {
	fakeLedger
	windows [][3]time.Duration
}

func (l *windowLedger) ClaimTranscriptionWindow(_ context.Context, _, _, _ string, _ uint64, start, split, end time.Duration) error {
	l.windows = append(l.windows, [3]time.Duration{start, split, end})
	return nil
}

func TestOverlapWindowUsesOriginalPCMAndDeduplicatesSharedSpeech(t *testing.T) {
	ctx := context.Background()
	m := &fakeMeter{active: true}
	l := &windowLedger{}
	calls := 0
	w := Worker{Authority: auth.ChannelContext{AccountID: "a", ChannelID: "one", LiveSessionID: "s", Generation: 1}, Meter: m, Ledger: l, SoleBroadcaster: true, Overlap: true, NowOffset: func() time.Duration { return time.Hour }, Transcriber: fakeTranscriber(func(_ context.Context, b []byte) (string, error) {
		calls++
		duration, err := audio.WAVDuration(b)
		if err != nil {
			t.Fatal(err)
		}
		if calls == 1 {
			if duration != 5*time.Second {
				t.Fatal(duration)
			}
			return "I promise I", nil
		}
		if duration != 10*time.Second {
			t.Fatalf("joint audio duration %v", duration)
		}
		if calls == 2 && (b[44] != 11 || b[44+160000] != 22) {
			t.Fatal("original PCM changed or reordered")
		}
		return "I promise I will always pick the blue card.", nil
	})}
	first := fiveSecondWAV()
	first[44] = 11
	second := fiveSecondWAV()
	second[44] = 22
	for i, b := range [][]byte{first, second, fiveSecondWAV()} {
		if err := w.Submit(ctx, Chunk{Sequence: uint64(i + 1), Start: time.Duration(i) * 5 * time.Second, End: time.Duration(i+1) * 5 * time.Second, WAV: b}); err != nil {
			t.Fatal(err)
		}
	}
	if len(l.windows) != 2 || l.windows[0] != [3]time.Duration{0, 5 * time.Second, 10 * time.Second} {
		t.Fatalf("windows %#v", l.windows)
	}
	if len(l.segments) != 2 || l.segments[1].Start != 0 || l.segments[1].End != 10*time.Second || l.segments[1].Attribution != "broadcaster" {
		t.Fatalf("segments %#v", l.segments)
	}
	if m.calls != 3 {
		t.Fatalf("calls %d", m.calls)
	}
}

func TestOverlapDoesNotBridgeAudioGaps(t *testing.T) {
	m := &fakeMeter{active: true}
	l := &windowLedger{}
	w := Worker{Authority: auth.ChannelContext{LiveSessionID: "s", Generation: 1}, Meter: m, Ledger: l, Overlap: true, NowOffset: func() time.Duration { return time.Hour }, Transcriber: fakeTranscriber(func(_ context.Context, b []byte) (string, error) {
		d, _ := audio.WAVDuration(b)
		if d != time.Second {
			t.Fatal("gap joined")
		}
		return "words", nil
	})}
	for i, start := range []time.Duration{0, 3 * time.Second} {
		if err := w.Submit(context.Background(), Chunk{Sequence: uint64(i + 1), Start: start, End: start + time.Second, WAV: testWAV()}); err != nil {
			t.Fatal(err)
		}
	}
	if len(l.windows) != 0 || len(l.segments) != 2 {
		t.Fatal("gap provenance incorrect")
	}
}

func fiveSecondWAV() []byte {
	b := append(testWAV(), make([]byte, 4*32000)...)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	binary.LittleEndian.PutUint32(b[40:], 160000)
	return b
}

package replay

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"receipts/internal/ports"
	"receipts/internal/receipt"
)

type sinkFunc func(context.Context, ports.TranscriptSegment) error

func (f sinkFunc) AcceptTranscript(ctx context.Context, in ports.TranscriptSegment) error {
	return f(ctx, in)
}

func TestRunDispatchesInFixtureOrderAndAdvancesClock(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var got []string
	f := Fixture{Version: 1, Inputs: []Input{
		{At: base.Add(time.Second), Kind: "transcript", Transcript: &ports.TranscriptSegment{ID: "s1", SessionID: "session", Text: "one", Attribution: ports.AttributionBroadcaster}},
		{At: base.Add(2 * time.Second), Kind: "transcript", Transcript: &ports.TranscriptSegment{ID: "s2", SessionID: "session", Text: "two", Attribution: ports.AttributionBroadcaster}},
	}}
	clock := NewClock(base)
	err := f.Run(context.Background(), Sinks{Transcript: sinkFunc(func(_ context.Context, in ports.TranscriptSegment) error {
		got = append(got, in.ID)
		if !clock.Now().Equal(base.Add(time.Duration(len(got)) * time.Second)) {
			t.Fatalf("clock = %v", clock.Now())
		}
		return nil
	})}, clock)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"s1", "s2"}) {
		t.Fatalf("dispatch order = %v", got)
	}
}

func TestValidateRejectsAmbiguousAndUnorderedInputs(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	valid := Input{At: now, Kind: "transcript", Transcript: &ports.TranscriptSegment{ID: "s", SessionID: "session", Text: "hello", Attribution: ports.AttributionBroadcaster}}
	if err := (Fixture{Version: 1, Inputs: []Input{valid, valid}}).Validate(); err != nil {
		t.Fatalf("equal timestamps are allowed and ordered by file position: %v", err)
	}
	bad := valid
	bad.Chat = &ports.ChatCommand{}
	if err := (Fixture{Version: 1, Inputs: []Input{bad}}).Validate(); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("ambiguous payload error = %v", err)
	}
	later := valid
	later.At = now.Add(-time.Second)
	if err := (Fixture{Version: 1, Inputs: []Input{valid, later}}).Validate(); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("out-of-order error = %v", err)
	}
}

type countSink struct {
	calls  int
	onCall func()
}

func (s *countSink) AcceptTranscript(context.Context, ports.TranscriptSegment) error {
	s.calls++
	if s.onCall != nil {
		s.onCall()
	}
	return nil
}

func TestValidateChecksTypedFieldsAndEnums(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	valid := []Input{
		{At: now, Kind: "transcript", Transcript: &ports.TranscriptSegment{ID: "segment", SessionID: "session", Text: "line", Attribution: ports.AttributionUncertain}},
		{At: now, Kind: "chat", Chat: &ports.ChatCommand{ID: "chat", ChannelID: "channel", SessionID: "session", UserID: "user", Login: "viewer", Role: ports.ChatViewer, Command: "!receipt", ReceivedAt: now}},
		{At: now, Kind: "clip", Clip: &ports.ClipResult{RequestID: "request", CreatedAt: now, Status: ports.ClipPending}},
		{At: now, Kind: "session", Session: &ports.LiveSessionChange{SessionID: "session", ChannelID: "channel", State: ports.SessionLive, ObservedAt: now}},
	}
	for _, input := range valid {
		if err := (Fixture{Version: 1, Inputs: []Input{input}}).Validate(); err != nil {
			t.Errorf("valid %s input rejected: %v", input.Kind, err)
		}
	}
	invalid := append([]Input(nil), valid...)
	invalid[0].Transcript.Attribution = "speaker"
	invalid[1].Chat.Role = "owner"
	invalid[2].Clip.Status = "queued"
	invalid[3].Session.State = "starting"
	for _, input := range invalid {
		if err := (Fixture{Version: 1, Inputs: []Input{input}}).Validate(); !errors.Is(err, ErrInvalidFixture) {
			t.Errorf("invalid %s input error=%v", input.Kind, err)
		}
	}
}

func TestRunPreflightsAllSinksAndTypedInputsBeforeDispatch(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := Input{At: now, Kind: "transcript", Transcript: &ports.TranscriptSegment{ID: "s1", SessionID: "session", Text: "hello", Attribution: ports.AttributionBroadcaster}}
	second := Input{At: now.Add(time.Second), Kind: "transcript", Transcript: &ports.TranscriptSegment{ID: "s2", SessionID: "session", Text: "world", Attribution: ports.AttributionBroadcaster}}
	f := Fixture{Version: 1, Inputs: []Input{first, second}}
	sink := &countSink{}
	clock := NewClock(now.Add(-time.Second))
	if err := f.Run(context.Background(), Sinks{Transcript: sink}, clock); err != nil {
		t.Fatal(err)
	}
	if sink.calls != 2 {
		t.Fatalf("calls=%d", sink.calls)
	}

	// A valid first transcript must not dispatch before discovering the missing
	// chat boundary later in the fixture.
	chat := Input{At: now.Add(time.Second), Kind: "chat", Chat: &ports.ChatCommand{}}
	f.Inputs = []Input{first, chat}
	sink.calls = 0
	if err := f.Run(context.Background(), Sinks{Transcript: sink}, NewClock(now.Add(-time.Second))); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("missing sink error=%v", err)
	}
	if sink.calls != 0 {
		t.Fatalf("dispatch happened before sink preflight: %d", sink.calls)
	}

	// A malformed later payload is rejected before any earlier callback too.
	f.Inputs = []Input{first, second}
	f.Inputs[1].Transcript.Attribution = "chat"
	if err := f.Run(context.Background(), Sinks{Transcript: sink}, NewClock(now.Add(-time.Second))); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("invalid payload error=%v", err)
	}
	if sink.calls != 0 {
		t.Fatalf("dispatch happened before validation: %d", sink.calls)
	}
}

func TestRunChecksCanceledContextBeforeEachInput(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := Fixture{Version: 1, Inputs: []Input{{At: now, Kind: "transcript", Transcript: &ports.TranscriptSegment{ID: "s", SessionID: "session", Text: "hello", Attribution: ports.AttributionBroadcaster}}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sink := &countSink{}
	err := f.Run(ctx, Sinks{Transcript: sink}, NewClock(now.Add(-time.Second)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if sink.calls != 0 {
		t.Fatalf("canceled input dispatched %d times", sink.calls)
	}
	f.Inputs = append(f.Inputs, f.Inputs[0])
	f.Inputs[1].At = now.Add(time.Second)
	ctx, cancel = context.WithCancel(context.Background())
	sink.onCall = cancel
	err = f.Run(ctx, Sinks{Transcript: sink}, NewClock(now.Add(-time.Second)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-replay cancellation error=%v", err)
	}
	if sink.calls != 1 {
		t.Fatalf("dispatch continued after cancellation: calls=%d", sink.calls)
	}
}

func TestIDsAreIndependentStableAndKeelShaped(t *testing.T) {
	a, b := IDs(), IDs()
	for want := 1; want <= 3; want++ {
		gotA, err := a()
		if err != nil {
			t.Fatal(err)
		}
		gotB, err := b()
		if err != nil {
			t.Fatal(err)
		}
		if gotA != gotB || len(gotA) != 32 {
			t.Fatalf("IDs differ or malformed: %q / %q", gotA, gotB)
		}
		if gotA != fmt.Sprintf("%032x", want) {
			t.Fatalf("ID %d = %q", want, gotA)
		}
	}
}

func TestFoundationReplayPersistsIdenticalTranscriptsToFreshLedgers(t *testing.T) {
	fixtureFile, err := os.Open("../../testdata/replay/foundation.json")
	if err != nil {
		t.Fatal(err)
	}
	f, err := Decode(fixtureFile)
	_ = fixtureFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	run := func(name string) []receipt.Segment {
		t.Helper()
		clock := NewClock(f.Inputs[0].At.Add(-time.Second))
		ledger, err := OpenLedger(context.Background(), filepath.Join(t.TempDir(), name+".db"), clock)
		if err != nil {
			t.Fatal(err)
		}
		defer ledger.Close()
		if err := f.Run(context.Background(), Sinks{Transcript: TranscriptSink{Ledger: ledger}}, clock); err != nil {
			t.Fatal(err)
		}
		rows, err := ledger.Store.Transcript(context.Background(), ledger.Session.ID)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	one, two := run("first"), run("second")
	if !reflect.DeepEqual(one, two) {
		t.Fatalf("fresh ledger results differ:\n%+v\n%+v", one, two)
	}
	if len(one) != 1 || one[0].ID != "segment-1" || one[0].Text != "I will never pick that character again." {
		t.Fatalf("unexpected persisted transcript: %+v", one)
	}
}

func TestInjectedIDsPersistIdenticallyAcrossReceiptEvidenceAndVerdict(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	create := func(name string) []string {
		t.Helper()
		clock := NewClock(base.Add(-time.Second))
		ledger, err := OpenLedger(context.Background(), filepath.Join(t.TempDir(), name+".db"), clock)
		if err != nil {
			t.Fatal(err)
		}
		defer ledger.Close()
		seg := receipt.Segment{ID: "source-1", SessionID: ledger.Session.ID, Start: 0, End: 2 * time.Second, Text: "I will never do that again.", SpeakerID: "broadcaster", Attribution: string(ports.AttributionBroadcaster)}
		if err := ledger.Store.AddSegment(context.Background(), seg); err != nil {
			t.Fatal(err)
		}
		r, err := ledger.Store.CreateReceipt(context.Background(), receipt.ReceiptInput{ChannelID: ledger.Channel.ID, SessionID: ledger.Session.ID, Claim: "I will never do that again.", ClippedBy: "mod", Evidence: []receipt.EvidenceInput{{SourceID: seg.ID, Text: seg.Text, Start: seg.Start, End: seg.End, Attribution: seg.Attribution}}})
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := ledger.Store.Evidence(context.Background(), r.ID)
		if err != nil || len(evidence) != 1 {
			t.Fatalf("evidence=%+v err=%v", evidence, err)
		}
		verdict, err := ledger.Store.Decide(context.Background(), ledger.Channel.ID, r.ID, "decision-1", receipt.Admitted, "")
		if err != nil {
			t.Fatal(err)
		}
		return []string{r.ID, evidence[0].ID, verdict.ID}
	}
	one, two := create("first"), create("second")
	if !reflect.DeepEqual(one, two) {
		t.Fatalf("generated ledger IDs differ: %v / %v", one, two)
	}
	if want := []string{"00000000000000000000000000000003", "00000000000000000000000000000004", "00000000000000000000000000000005"}; !reflect.DeepEqual(one, want) {
		t.Fatalf("generated IDs=%v want %v", one, want)
	}
}

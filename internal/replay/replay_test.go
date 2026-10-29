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
		{At: base.Add(time.Second), Kind: "transcript", Transcript: &ports.TranscriptSegment{ID: "s1", Text: "one"}},
		{At: base.Add(2 * time.Second), Kind: "transcript", Transcript: &ports.TranscriptSegment{ID: "s2", Text: "two"}},
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
	valid := Input{At: now, Kind: "transcript", Transcript: &ports.TranscriptSegment{ID: "s"}}
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

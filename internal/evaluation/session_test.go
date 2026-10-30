package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/nrynss/keel/cost"
	"receipts/internal/commands"
	"receipts/internal/judge"
	"receipts/internal/providerusage"
	"receipts/internal/receipt"

	"receipts/internal/detect"
)

func TestSessionReplayUsesProductionService(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/eval/sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Sessions []SessionCase `json:"sessions"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, s := range fixture.Sessions {
		t.Run(s.ID, func(t *testing.T) {
			r := SessionRecording{Vectors: map[string][]float64{}, Sensitive: map[string]bool{}, Verdicts: map[string]detect.Verdict{}}
			r.Vectors[s.LaterID] = []float64{1, 0}
			r.Sensitive[s.LaterID] = s.LaterSensitive
			for i, c := range s.Candidates {
				r.Sensitive[c.ID] = c.Sensitive
				r.Vectors[c.ID] = []float64{1, float64(i + 1)}
				r.Verdicts[c.ID] = detect.Unsure
			}
			got, err := ReplaySession(context.Background(), t.TempDir(), s, r)
			if err != nil {
				t.Fatal(err)
			}
			if s.LaterAttribution == "uncertain" || s.LaterSensitive {
				if !got.SourceRejected || len(got.Judged) != 0 {
					t.Fatalf("unsafe source reached judge: %+v", got)
				}
				return
			}
			if len(got.Judged) != 3 || !got.TargetRetrieved {
				t.Fatalf("unexpected retrieval: %+v", got)
			}
			for _, v := range got.Outcomes {
				if v != "review" {
					t.Fatalf("unexpected outcome: %+v", got)
				}
			}
		})
	}
}

func TestSessionTraceRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/eval/sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Sessions []SessionCase `json:"sessions"`
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	s := f.Sessions[0]
	vectors := map[string][]float64{s.Later: {1, 0}}
	for i, c := range s.Candidates {
		vectors[c.Claim] = []float64{1, float64(i + 1)}
	}
	measure := func(ctx context.Context) { providerusage.Report(ctx, cost.Usage{Measured: true, Price: 1}) }
	base := SessionAdapters{Dimensions: 2, Budget: 500_000_000, Extractor: sessionExtract(func(ctx context.Context, segs []receipt.Segment) (commands.Extraction, error) {
		measure(ctx)
		x := segs[len(segs)-1]
		return commands.Extraction{Claim: x.Text, SourceIDs: []string{x.ID}}, nil
	}), Sensitive: sessionSensitive(func(ctx context.Context, _ string) (bool, error) { measure(ctx); return false, nil }), Embedder: sessionEmbed(func(ctx context.Context, text string) ([]float64, error) { measure(ctx); return vectors[text], nil }), Judge: sessionJudge(func(ctx context.Context, _ receipt.Receipt, e receipt.Evidence, l receipt.Segment) (judge.Result, error) {
		measure(ctx)
		return judge.Result{Verdict: detect.Unsure, ClaimSourceID: e.SourceID, LaterSourceID: l.ID}, nil
	})}
	trace := SessionTrace{SessionID: s.ID}
	first, err := RunSession(context.Background(), t.TempDir(), s, TraceAdapters(base, &trace))
	if err != nil {
		t.Fatal(err)
	}
	adapters, check := TraceReplay(trace, 2, 500_000_000)
	second, err := RunSession(context.Background(), t.TempDir(), s, adapters)
	if err != nil {
		t.Fatal(err)
	}
	if err = check(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Outcomes, second.Outcomes) || first.TargetRetrieved != second.TargetRetrieved {
		t.Fatalf("trace differs: %+v %+v", first, second)
	}
	trace.Events[0].InputSHA256 = "invalid"
	adapters, _ = TraceReplay(trace, 2, 500_000_000)
	if _, err = RunSession(context.Background(), t.TempDir(), s, adapters); err == nil {
		t.Fatal("tampered trace accepted")
	}
}

func TestPairSessionsNeverUseLabelsAndStayWithinSplit(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/eval/pairs.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	pairs, _, err := ReadPairs(raw)
	if err != nil {
		t.Fatal(err)
	}
	a, err := PairSessions(pairs, "dev")
	if err != nil {
		t.Fatal(err)
	}
	for i := range pairs {
		pairs[i].Label = "changed"
		pairs[i].Expected = "changed"
	}
	b, err := PairSessions(pairs, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) || len(a) != 34 {
		t.Fatal("labels affected session construction")
	}
	ids := map[string]bool{}
	for _, p := range pairs {
		if p.Split == "dev" {
			ids[p.ID] = true
		}
	}
	for _, s := range a {
		if len(s.Candidates) != 4 || s.Candidates[0].ID != s.TargetID {
			t.Fatal("invalid candidate pool")
		}
		for _, c := range s.Candidates {
			if !ids[c.ID] {
				t.Fatal("cross-split candidate")
			}
		}
	}
}

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

func TestFreezeRejectsIncompleteDevelopment(t *testing.T) {
	f := Freeze{DatasetSHA256: "dataset", Policy: "review-only-v1", DevReportSHA256: "report", JudgeSourceSHA256: "judge", PolicySourceSHA256: "policy"}
	valid := []byte(`{"DatasetSHA256":"dataset","Split":"dev","Policy":"review-only-v1","Total":34,"Errors":0,"Complete":true,"DiagnosticOnly":false}`)
	if err := ValidateDevelopmentFreeze(f, "dataset", "report", "judge", "policy", 34, valid); err != nil {
		t.Fatal(err)
	}
	for _, report := range []string{`{"DatasetSHA256":"dataset","Split":"dev","Policy":"review-only-v1","Total":1,"Complete":false}`, `{"DatasetSHA256":"dataset","Split":"dev","Policy":"review-only-v1","Total":34,"Complete":true,"DiagnosticOnly":true}`, `{"DatasetSHA256":"dataset","Split":"dev","Policy":"review-only-v1","Total":34,"Errors":1,"Complete":true}`} {
		if err := ValidateDevelopmentFreeze(f, "dataset", "report", "judge", "policy", 34, []byte(report)); err == nil {
			t.Fatalf("accepted incomplete report %s", report)
		}
	}
}

func TestCaptureWindowsIsolateEarliestChoosingExtractor(t *testing.T) {
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
	sc := fixture.Sessions[0]
	vectors := map[string][]float64{sc.Later: {1, 0}}
	want := make([]string, 0, len(sc.Candidates))
	for i, c := range sc.Candidates {
		vectors[c.Claim] = []float64{1, float64(i + 1)}
		want = append(want, c.ID)
	}
	var windows [][]string
	adapters := SessionAdapters{Dimensions: 2, Budget: 4_000_000_000, Extractor: sessionExtract(func(_ context.Context, segs []receipt.Segment) (commands.Extraction, error) {
		ids := make([]string, len(segs))
		for i, s := range segs {
			ids[i] = s.ID
		}
		windows = append(windows, ids)
		earliest := segs[0]
		return commands.Extraction{Claim: earliest.Text, SourceIDs: []string{earliest.ID}}, nil
	}), Sensitive: sessionSensitive(func(context.Context, string) (bool, error) { return false, nil }), Embedder: sessionEmbed(func(_ context.Context, text string) ([]float64, error) { return vectors[text], nil }), Judge: sessionJudge(func(_ context.Context, _ receipt.Receipt, e receipt.Evidence, l receipt.Segment) (judge.Result, error) {
		return judge.Result{Verdict: detect.Unsure, ClaimSourceID: e.SourceID, LaterSourceID: l.ID}, nil
	})}
	got, err := RunSession(context.Background(), t.TempDir(), sc, adapters)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Captured, want) || len(got.CapturedReceipts) != len(want) || len(got.Judged) != 3 {
		t.Fatalf("unexpected result: %+v", got)
	}
	seen := map[string]bool{}
	for i, m := range got.CapturedReceipts {
		if m.SourceID != want[i] || m.ReceiptID == "" || seen[m.ReceiptID] {
			t.Fatalf("invalid receipt mapping %+v", got.CapturedReceipts)
		}
		seen[m.ReceiptID] = true
	}
	if len(windows) != len(want) {
		t.Fatalf("windows=%v", windows)
	}
	for i, w := range windows {
		if len(w) != 1 || w[0] != want[i] {
			t.Fatalf("window %d contains %v", i, w)
		}
	}
}

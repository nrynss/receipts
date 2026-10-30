package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"receipts/internal/commands"
	"receipts/internal/detect"
	"receipts/internal/judge"
	"receipts/internal/providerusage"
	"receipts/internal/receipt"
)

func testManifest() SessionBundle {
	return SessionBundle{CorpusKind: "pairs", DatasetSHA256: "dataset", Split: "dev", Provenance: "synthetic", JudgeModel: "judge", EmbeddingModel: "embedding", SourceHashes: map[string]string{"source": "hash"}, Requested: 2, BudgetNanoUSD: 100_000_000}
}
func TestCheckpointRejectsInterruptedAttempts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.json")
	manifest := testManifest()
	c, err := OpenCheckpoint(path, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.StartCase("case"); err != nil {
		t.Fatal(err)
	}
	event := SessionEvent{Kind: "extract", InputSHA256: "input"}
	i, err := c.Begin(event, 25_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.validate(); err == nil {
		t.Fatal("pending request resumed")
	}
	event.State = "measured"
	event.Measured = true
	event.SpendNanoUSD = 12
	if err = c.Update(i, event); err != nil {
		t.Fatal(err)
	}
	if err = c.validate(); err == nil {
		t.Fatal("response-missing request resumed")
	}
	event.State = "complete"
	if err = c.Update(i, event); err != nil {
		t.Fatal(err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenCheckpoint(path, manifest, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	changed := manifest
	changed.SourceHashes = map[string]string{"source": "different"}
	if _, err = OpenCheckpoint(path, changed, true); err == nil {
		t.Fatal("changed source resumed")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("unsafe checkpoint permissions: %v %v", info, err)
	}
}
func TestResumeUsesRecordedEventsAndCumulativeBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.json")
	manifest := testManifest()
	c, err := OpenCheckpoint(path, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.StartCase("case"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	base := SessionAdapters{Dimensions: 2, Budget: 50_000_000, Extractor: sessionExtract(func(ctx context.Context, s []receipt.Segment) (commands.Extraction, error) {
		calls++
		providerusage.Report(ctx, cost.Usage{Measured: true, Price: 10})
		return commands.Extraction{Claim: s[0].Text, SourceIDs: []string{s[0].ID}}, nil
	}), Sensitive: sessionSensitive(func(context.Context, string) (bool, error) { t.Fatal("unexpected sensitivity call"); return false, nil }), Embedder: sessionEmbed(func(context.Context, string) ([]float64, error) {
		t.Fatal("unexpected embedding call")
		return nil, nil
	}), Judge: sessionJudge(func(context.Context, receipt.Receipt, receipt.Evidence, receipt.Segment) (judge.Result, error) {
		t.Fatal("unexpected judge call")
		return judge.Result{}, nil
	})}
	first := []receipt.Segment{{ID: "one", Text: "I will go.", Attribution: "broadcaster", End: time.Second}}
	second := []receipt.Segment{{ID: "two", Text: "I will stay.", Attribution: "broadcaster", End: time.Second}}
	if _, err = CheckpointAdapters(base, c).Extractor.Extract(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = OpenCheckpoint(path, manifest, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.StartCase("case"); err != nil {
		t.Fatal(err)
	}
	adapters, check := ResumeAdapters(base, c)
	if _, err = adapters.Extractor.Extract(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("replayed request called provider")
	}
	if _, err = adapters.Extractor.Extract(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("new request not made exactly once")
	}
	if err = check(); err != nil {
		t.Fatal(err)
	}
	spent, err := c.Spent()
	if err != nil || spent != 20 {
		t.Fatalf("spent=%d err=%v", spent, err)
	}
	if _, err = c.Begin(SessionEvent{Kind: "test", InputSHA256: "x"}, manifest.BudgetNanoUSD); err == nil {
		t.Fatal("cumulative budget ignored")
	}
}

func TestInterruptedSessionResumesWithoutRepeatingCalls(t *testing.T) {
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
	manifest := testManifest()
	manifest.Requested = 1
	manifest.BudgetNanoUSD = 4_000_000_000
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	c, err := OpenCheckpoint(path, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.StartCase(sc.ID); err != nil {
		t.Fatal(err)
	}
	vectors := map[string][]float64{sc.Later: {1, 0}}
	for i, x := range sc.Candidates {
		vectors[x.Claim] = []float64{1, float64(i + 1)}
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	interrupt := true
	measure := func(ctx context.Context) { providerusage.Report(ctx, cost.Usage{Measured: true, Price: 1}) }
	base := SessionAdapters{Dimensions: 2, Budget: 500_000_000, Extractor: sessionExtract(func(ctx context.Context, s []receipt.Segment) (commands.Extraction, error) {
		calls++
		measure(ctx)
		v := s[0]
		if interrupt && calls == 2 {
			cancel()
		}
		return commands.Extraction{Claim: v.Text, SourceIDs: []string{v.ID}}, nil
	}), Sensitive: sessionSensitive(func(ctx context.Context, _ string) (bool, error) { measure(ctx); return false, nil }), Embedder: sessionEmbed(func(ctx context.Context, text string) ([]float64, error) { measure(ctx); return vectors[text], nil }), Judge: sessionJudge(func(ctx context.Context, _ receipt.Receipt, e receipt.Evidence, l receipt.Segment) (judge.Result, error) {
		measure(ctx)
		return judge.Result{Verdict: detect.Unsure, ClaimSourceID: e.SourceID, LaterSourceID: l.ID}, nil
	})}
	a, _ := ResumeAdapters(base, c)
	if _, err = RunSession(ctx, t.TempDir(), sc, a); err == nil {
		t.Fatal("interrupted run succeeded")
	}
	if err = c.Stop("deadline"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("unexpected first-run calls: %d", calls)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = OpenCheckpoint(path, manifest, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.StartCase(sc.ID); err != nil {
		t.Fatal(err)
	}
	interrupt = false
	a, check := ResumeAdapters(base, c)
	result, err := RunSession(context.Background(), t.TempDir(), sc, a)
	if err != nil {
		t.Fatal(err)
	}
	if err = check(); err != nil {
		t.Fatal(err)
	}
	if calls != 4 || len(result.Captured) != 4 {
		t.Fatalf("duplicate calls or missing capture: calls=%d result=%+v", calls, result)
	}
	if err = c.FinishCase(result, 0); err != nil {
		t.Fatal(err)
	}
	if err = c.Complete(); err != nil {
		t.Fatal(err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = OpenCheckpoint(path, manifest, true)
	if err != nil || c.Bundle.RunStatus != "complete" {
		t.Fatalf("completed checkpoint: %v", err)
	}
}

func TestBudgetPreflightAndUnknownUsageStopBeforeRetry(t *testing.T) {
	manifest := testManifest()
	manifest.BudgetNanoUSD = 20_000_000
	c, err := OpenCheckpoint(filepath.Join(t.TempDir(), "budget.json"), manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.StartCase("case"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	base := SessionAdapters{Extractor: sessionExtract(func(context.Context, []receipt.Segment) (commands.Extraction, error) {
		calls++
		return commands.Extraction{}, nil
	})}
	seg := []receipt.Segment{{ID: "s", Text: "I will go.", Attribution: "broadcaster", End: time.Second}}
	if _, err = CheckpointAdapters(base, c).Extractor.Extract(context.Background(), seg); err == nil || calls != 0 {
		t.Fatal("budget preflight called provider")
	}
	manifest.BudgetNanoUSD = 100_000_000
	path := filepath.Join(t.TempDir(), "unknown.json")
	c, err = OpenCheckpoint(path, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.StartCase("case"); err != nil {
		t.Fatal(err)
	}
	if _, err = CheckpointAdapters(base, c).Extractor.Extract(context.Background(), seg); err == nil || calls != 1 {
		t.Fatal("missing usage did not stop")
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = OpenCheckpoint(path, manifest, true); err == nil {
		t.Fatal("unknown usage resumed")
	}
}

func TestCheckpointPreventsConcurrentCollectors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.json")
	m := testManifest()
	c, err := OpenCheckpoint(path, m, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = OpenCheckpoint(path, m, true); err == nil {
		t.Fatal("concurrent collector acquired checkpoint")
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = OpenCheckpoint(path, m, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRemainingSkipsCompletedCasesAndChecksOrder(t *testing.T) {
	m := testManifest()
	c, err := OpenCheckpoint(filepath.Join(t.TempDir(), "run.json"), m, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.StartCase("a"); err != nil {
		t.Fatal(err)
	}
	if err = c.FinishCase(SessionResult{}, 0); err != nil {
		t.Fatal(err)
	}
	pending, err := c.Remaining([]SessionCase{{ID: "a"}, {ID: "b"}})
	if err != nil || len(pending) != 1 || pending[0].ID != "b" {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	if _, err = c.Remaining([]SessionCase{{ID: "b"}, {ID: "a"}}); err == nil {
		t.Fatal("changed case order accepted")
	}
}

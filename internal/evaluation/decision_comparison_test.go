package evaluation

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nrynss/keel/cost"
	"receipts/internal/claims"
	"receipts/internal/detect"
	"receipts/internal/judge"
	"receipts/internal/providerusage"
	"receipts/internal/receipt"
)

type comparisonJudgeFunc func(context.Context, receipt.Receipt, receipt.Evidence, receipt.Segment) (judge.Result, error)

func (f comparisonJudgeFunc) Judge(ctx context.Context, r receipt.Receipt, e receipt.Evidence, l receipt.Segment) (judge.Result, error) {
	return f(ctx, r, e, l)
}

func TestDecisionComparisonCheckpointsBothSanitizedAnswersAndUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comparison.json")
	manifest := SessionBundle{CorpusKind: "pairs-decisions-comparison", DatasetSHA256: "dataset", Split: "dev", Provenance: "synthetic", JudgeModel: judge.DecisionsModel, SourceHashes: map[string]string{"judge": "source"}, Requested: 1, BudgetNanoUSD: 1000}
	c, err := OpenCheckpoint(path, manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	old := comparisonJudgeFunc(func(ctx context.Context, _ receipt.Receipt, e receipt.Evidence, l receipt.Segment) (judge.Result, error) {
		providerusage.Report(ctx, cost.Usage{Measured: true, Price: 100})
		return judge.Result{Verdict: detect.Unsure, ClaimSourceID: e.SourceID, LaterSourceID: l.ID}, nil
	})
	new := comparisonJudgeFunc(func(ctx context.Context, _ receipt.Receipt, e receipt.Evidence, l receipt.Segment) (judge.Result, error) {
		confidence := 0.8
		providerusage.Report(ctx, cost.Usage{Measured: true, Price: 20})
		return judge.Result{Verdict: detect.Contradicts, ClaimSourceID: e.SourceID, LaterSourceID: l.ID, Confidence: &confidence, Probabilities: map[string]float64{"contradicts": 0.8, "does_not_contradict": 0.1, "unsure": 0.1}}, nil
	})
	oldSensitivity := comparisonSensitivityFunc(func(ctx context.Context, _ string) (claims.SensitivityDecision, error) {
		providerusage.Report(ctx, cost.Usage{Measured: true, Price: 100})
		return claims.SensitivityDecision{Choice: "not_sensitive"}, nil
	})
	newSensitivity := comparisonSensitivityFunc(func(ctx context.Context, _ string) (claims.SensitivityDecision, error) {
		providerusage.Report(ctx, cost.Usage{Measured: true, Price: 20})
		return claims.SensitivityDecision{Choice: "not_sensitive", Confidence: 0.9, Probabilities: map[string]float64{"sensitive": 0.1, "not_sensitive": 0.8, "unsure": 0.1}}, nil
	})
	pair := Pair{ID: "P0-001", Split: "dev", Claim: "I will never use the blue map.", Later: "I used the blue map.", Label: "contradiction"}
	if err := CompareDecisionCase(context.Background(), c, pair, oldSensitivity, newSensitivity, old, new, 500, 500); err != nil {
		t.Fatal(err)
	}
	if err := c.Complete(); err != nil {
		t.Fatal(err)
	}
	if len(c.Bundle.Sessions) != 1 || c.Bundle.Sessions[0].Result.Outcomes["legacy_verdict"] != string(detect.Unsure) || c.Bundle.Sessions[0].Result.Outcomes["decisions_verdict"] != string(detect.Contradicts) {
		t.Fatalf("comparison result=%+v", c.Bundle.Sessions)
	}
	if len(c.Bundle.Sessions[0].Trace.Events) != 6 || c.Bundle.Sessions[0].Trace.Events[5].Judgment.Confidence == nil || c.Bundle.Sessions[0].Trace.Events[0].Sensitive || c.Bundle.Sessions[0].Trace.Events[0].RequestSHA256 != "" {
		t.Fatalf("trace=%+v", c.Bundle.Sessions[0].Trace)
	}
	spent, err := c.Spent()
	if err != nil || spent != 360 {
		t.Fatalf("spent=%d err=%v", spent, err)
	}
}

type comparisonSensitivityFunc func(context.Context, string) (claims.SensitivityDecision, error)

func (f comparisonSensitivityFunc) Classify(ctx context.Context, text string) (claims.SensitivityDecision, error) {
	return f(ctx, text)
}

func TestDecisionComparisonSuppressesBothJudgesWhenEitherSensitivityAdapterFlagsText(t *testing.T) {
	manifest := SessionBundle{CorpusKind: "pairs-decisions-comparison", DatasetSHA256: "dataset", Split: "dev", Provenance: "synthetic", JudgeModel: judge.DecisionsModel, SourceHashes: map[string]string{"source": "hash"}, Requested: 1, BudgetNanoUSD: 1000}
	c, err := OpenCheckpoint(filepath.Join(t.TempDir(), "comparison.json"), manifest, false)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	oldSensitivity := comparisonSensitivityFunc(func(ctx context.Context, text string) (claims.SensitivityDecision, error) {
		providerusage.Report(ctx, cost.Usage{Measured: true, Price: 10})
		if text == "private claim" {
			return claims.SensitivityDecision{Choice: "sensitive"}, nil
		}
		return claims.SensitivityDecision{Choice: "not_sensitive"}, nil
	})
	newSensitivity := comparisonSensitivityFunc(func(ctx context.Context, _ string) (claims.SensitivityDecision, error) {
		providerusage.Report(ctx, cost.Usage{Measured: true, Price: 10})
		return claims.SensitivityDecision{Choice: "not_sensitive"}, nil
	})
	noJudge := comparisonJudgeFunc(func(context.Context, receipt.Receipt, receipt.Evidence, receipt.Segment) (judge.Result, error) {
		t.Fatal("sensitivity flag must prevent both judge calls")
		return judge.Result{}, nil
	})
	pair := Pair{ID: "P0-sensitive", Split: "dev", Claim: "private claim", Later: "public later"}
	if err := CompareDecisionCase(context.Background(), c, pair, oldSensitivity, newSensitivity, noJudge, noJudge, 500, 500); err != nil {
		t.Fatal(err)
	}
	if err := c.Complete(); err != nil {
		t.Fatal(err)
	}
	if got := c.Bundle.Sessions[0].Result.Outcomes["legacy_claim_sensitive"]; got != "true" {
		t.Fatalf("legacy sensitivity=%q", got)
	}
	if len(c.Bundle.Sessions[0].Trace.Events) != 4 {
		t.Fatalf("trace events=%d, want four sensitivity events and no judge events", len(c.Bundle.Sessions[0].Trace.Events))
	}
}

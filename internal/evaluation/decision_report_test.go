package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"receipts/internal/detect"
	"receipts/internal/judge"
)

func TestOwnerReviewOverlayIsBoundAndAppliedWithoutChangingOriginal(t *testing.T) {
	dataset, err := os.ReadFile("../../testdata/eval/pairs.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	progress, err := os.ReadFile("../../docs/t54-label-review-progress.json")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := os.ReadFile("../../docs/evidence/t54-human-reviewed-report.json")
	if err != nil {
		t.Fatal(err)
	}
	original := sha256.Sum256(dataset)
	pairs, hashes, err := ReadOwnerReviewedPairs(dataset, progress, summary)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(original[:]) != hashes.DatasetSHA256 {
		t.Fatal("dataset hash changed")
	}
	found := false
	for _, p := range pairs {
		if p.ID == "P0-028" {
			found = p.Label == "contradiction" && p.Expected == "stamp"
		}
	}
	if !found {
		t.Fatal("owner correction was not applied")
	}
	if sha256.Sum256(dataset) != original {
		t.Fatal("overlay mutated original bytes")
	}
	if _, _, err = ReadOwnerReviewedPairs(dataset, progress, []byte(`{}`)); err == nil {
		t.Fatal("accepted unbound review summary")
	}
}

func TestDecisionScoringReportsFalsePositiveAndNeedsHeldoutFreeze(t *testing.T) {
	pair := Pair{ID: "dev-1", Split: "dev", Claim: "I will finish the route.", Later: "I stopped before the end.", Label: "no_contradiction", Expected: "quiet"}
	pairs := []Pair{pair}
	threshold := DecisionThreshold{MinConfidence: .5, MinProbability: .5}
	confidence := .9
	probabilities := map[string]float64{"contradicts": .8, "does_not_contradict": .1, "unsure": .1}
	sensitivityProb := map[string]float64{"sensitive": .01, "not_sensitive": .98, "unsure": .01}
	events := []SessionEvent{}
	for _, item := range []struct {
		kind, model string
		choice      string
		probs       map[string]float64
	}{
		{"sensitive-comparison-legacy-claim", "gpt-4o-mini-2024-07-18", "not_sensitive", nil},
		{"sensitive-comparison-legacy-later", "gpt-4o-mini-2024-07-18", "not_sensitive", nil},
		{"sensitive-comparison-decisions-claim", judge.DecisionsModel, "not_sensitive", sensitivityProb},
		{"sensitive-comparison-decisions-later", judge.DecisionsModel, "not_sensitive", sensitivityProb},
	} {
		events = append(events, SessionEvent{Kind: item.kind, Model: item.model, Choice: item.choice, Confidence: &confidence, Probabilities: item.probs, State: "complete", Measured: true, RequestSHA256: "request", InputSHA256: "input"})
	}
	judgment := judge.Result{Verdict: detect.Contradicts, ClaimSourceID: pair.ID + ":claim", LaterSourceID: pair.ID + ":later", Confidence: &confidence, Probabilities: probabilities}
	for _, item := range []struct {
		kind, model string
		result      judge.Result
	}{
		{"judge-comparison-legacy", "gpt-4o-mini-2024-07-18", judge.Result{Verdict: detect.Contradicts}},
		{"judge-comparison-decisions", judge.DecisionsModel, judgment},
	} {
		events = append(events, SessionEvent{Kind: item.kind, Model: item.model, Judgment: item.result, State: "complete", Measured: true, RequestSHA256: "request", InputSHA256: "input"})
	}
	bundle := SessionBundle{CorpusKind: "pairs-decisions-comparison", DatasetSHA256: "dataset", Split: "dev", Provenance: "live-provider-sanitized", JudgeModel: judge.DecisionsModel, ComparisonModels: map[string]string{"legacy_sensitivity": "gpt-4o-mini-2024-07-18", "decisions_sensitivity": judge.DecisionsModel, "legacy_judge": "gpt-4o-mini-2024-07-18", "decisions_judge": judge.DecisionsModel}, SourceHashes: map[string]string{"source": "hash"}, Requested: 1, RunStatus: "complete", Sessions: []SessionRecordedCase{{ID: pair.ID, Trace: SessionTrace{Events: events}}}}
	checkpoint, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	report, err := ScoreDecisionComparison(pairs, DecisionOverlayHashes{DatasetSHA256: "dataset", ReviewSHA256: "review"}, bundle, checkpoint, "scorer", &threshold)
	if err != nil {
		t.Fatal(err)
	}
	if report.Selected == nil || report.Selected.FalseStamps != 1 || report.Selected.Precision == nil || *report.Selected.Precision != 0 || len(report.Selected.FalseStampIDs) != 1 || report.Selected.FalseStampIDs[0] != pair.ID || len(report.Inspections) != 1 || !report.Inspections[0].FalseStamp {
		t.Fatalf("expected inspectable false positive, got %+v", report)
	}
	bundle.Split = "heldout"
	checkpoint, _ = json.Marshal(bundle)
	if _, err = ScoreDecisionComparison(pairs, DecisionOverlayHashes{DatasetSHA256: "dataset", ReviewSHA256: "review"}, bundle, checkpoint, "scorer", nil); err == nil {
		t.Fatal("heldout scoring accepted unfrozen threshold")
	}
}

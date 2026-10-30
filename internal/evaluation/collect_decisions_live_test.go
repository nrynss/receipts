//go:build live

package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"testing"

	"receipts/internal/claims"
	"receipts/internal/judge"
)

const legacyJudgeReservationNanoUSD int64 = 25_000_000
const decisionsJudgeReservationNanoUSD int64 = 1_000_000
const comparisonCaseReservationNanoUSD int64 = 3*legacyJudgeReservationNanoUSD + 3*decisionsJudgeReservationNanoUSD

// TestCollectDecisionsComparison is opt-in and paid. Start with MAX_NEW_CASES=1
// while CASES names the full fixed partition; the checkpoint can then continue
// under the same budget and manifest without repeating that first case.
func TestCollectDecisionsComparison(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY required")
	}
	split := os.Getenv("RECEIPTS_DECISIONS_EVAL_SPLIT")
	if split != "dev" && split != "heldout" {
		t.Fatal("set RECEIPTS_DECISIONS_EVAL_SPLIT to dev or heldout")
	}
	caseCount := envInt(t, "RECEIPTS_DECISIONS_EVAL_CASES", 0)
	maxNew := envInt(t, "RECEIPTS_DECISIONS_EVAL_MAX_NEW_CASES", 1)
	budget, err := strconv.ParseInt(os.Getenv("RECEIPTS_DECISIONS_EVAL_BUDGET_NANO_USD"), 10, 64)
	if err != nil {
		t.Fatal("set RECEIPTS_DECISIONS_EVAL_BUDGET_NANO_USD to an approved positive ceiling")
	}
	path := os.Getenv("RECEIPTS_DECISIONS_EVAL_CHECKPOINT")
	if path == "" {
		t.Fatal("set RECEIPTS_DECISIONS_EVAL_CHECKPOINT to a new mode-0600 output path")
	}
	resume := os.Getenv("RECEIPTS_DECISIONS_EVAL_RESUME") == "1"
	if v := os.Getenv("RECEIPTS_DECISIONS_EVAL_RESUME"); v != "" && v != "1" {
		t.Fatal("RECEIPTS_DECISIONS_EVAL_RESUME must be 1 or empty")
	}
	raw, err := os.ReadFile("../../testdata/eval/pairs.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	pairs, datasetHash, err := ReadPairs(raw)
	if err != nil {
		t.Fatal(err)
	}
	var selected []Pair
	for _, pair := range pairs {
		if pair.Split == split {
			selected = append(selected, pair)
		}
	}
	maxCases := len(selected)
	if caseCount < 1 || caseCount > maxCases || maxNew < 1 || maxNew > caseCount || budget < comparisonCaseReservationNanoUSD || budget > int64(caseCount)*comparisonCaseReservationNanoUSD {
		t.Fatalf("invalid cases, per-invocation count, or bounded approved budget (partition has %d cases)", maxCases)
	}
	selected = selected[:caseCount]
	caseSequence := make([]SessionCase, len(selected))
	for i, pair := range selected {
		caseSequence[i] = SessionCase{ID: pair.ID, Split: split}
	}
	readSource := func(name string) string {
		b, e := os.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		h := sha256.Sum256(b)
		return hex.EncodeToString(h[:])
	}
	sources := map[string]string{
		"legacy_judge":            readSource("../judge/openai.go"),
		"decisions_judge":         readSource("../judge/decisions.go"),
		"legacy_sensitivity":      readSource("../claims/openai.go"),
		"legacy_sensitivity_impl": readSource("../claims/sensitive.go"),
		"decisions_sensitivity":   readSource("../claims/decisions.go"),
		"usage":                   readSource("../providerusage/usage.go"),
		"comparison":              readSource("decision_comparison.go"),
		"checkpoint":              readSource("checkpoint.go"),
		"trace_schema":            readSource("trace.go"),
		"bundle_schema":           readSource("session_bundle.go"),
		"live_runner":             readSource("collect_decisions_live_test.go"),
	}
	models := map[string]string{"legacy_sensitivity": "gpt-4o-mini-2024-07-18", "decisions_sensitivity": judge.DecisionsModel, "legacy_judge": "gpt-4o-mini-2024-07-18", "decisions_judge": judge.DecisionsModel}
	manifest := SessionBundle{CorpusKind: "pairs-decisions-comparison", DatasetSHA256: datasetHash, Split: split, Provenance: "live-provider-sanitized", JudgeModel: judge.DecisionsModel, ComparisonModels: models, SourceHashes: sources, Requested: caseCount, BudgetNanoUSD: budget}
	if split == "heldout" {
		freezePath := os.Getenv("RECEIPTS_DECISIONS_EVAL_FREEZE")
		devCheckpoint := os.Getenv("RECEIPTS_DECISIONS_EVAL_DEV_CHECKPOINT")
		if freezePath == "" || devCheckpoint == "" {
			t.Fatal("held-out comparison requires a frozen development checkpoint and freeze manifest")
		}
		devBundleRaw := readFileFatal(t, devCheckpoint)
		freezeRaw := readFileFatal(t, freezePath)
		var dev SessionBundle
		var freeze struct {
			DatasetSHA256 string            `json:"dataset_sha256"`
			DevSHA256     string            `json:"dev_checkpoint_sha256"`
			JudgeModel    string            `json:"judge_model"`
			SourceHashes  map[string]string `json:"source_hashes"`
			Policy        string            `json:"policy"`
			Models        map[string]string `json:"comparison_models"`
		}
		if json.Unmarshal(devBundleRaw, &dev) != nil || json.Unmarshal(freezeRaw, &freeze) != nil || dev.Split != "dev" || dev.RunStatus != "complete" || dev.DatasetSHA256 != datasetHash || dev.JudgeModel != judge.DecisionsModel || !sameStringMap(dev.SourceHashes, sources) {
			t.Fatal("invalid development checkpoint")
		}
		devHash := sha256.Sum256(devBundleRaw)
		if freeze.DatasetSHA256 != datasetHash || freeze.DevSHA256 != hex.EncodeToString(devHash[:]) || freeze.JudgeModel != judge.DecisionsModel || freeze.Policy != "review-only-v1" || !sameStringMap(freeze.SourceHashes, sources) || !sameStringMap(freeze.Models, models) || !sameStringMap(dev.ComparisonModels, models) {
			t.Fatal("invalid held-out freeze manifest")
		}
		freezeHash := sha256.Sum256(freezeRaw)
		manifest.FreezeSHA256 = hex.EncodeToString(freezeHash[:])
	}
	checkpoint, err := OpenCheckpoint(path, manifest, resume)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := checkpoint.Close(); err != nil {
			t.Error("checkpoint lock release failed")
		}
	}()
	pending, err := checkpoint.Remaining(caseSequence)
	if err != nil {
		t.Fatal("checkpoint case sequence mismatch")
	}
	if len(pending) > maxNew {
		pending = pending[:maxNew]
	}
	httpClient := &http.Client{Transport: DigestTransport{}}
	legacy := judge.Client{APIKey: key, HTTP: httpClient}
	decisions := judge.DecisionsClient{APIKey: key, HTTP: httpClient}
	legacySensitivity := legacySensitivityAdapter{Client: claims.Client{APIKey: key, HTTP: httpClient}}
	decisionsSensitivity := claims.DecisionsClient{APIKey: key, HTTP: httpClient}
	for _, sc := range pending {
		var pair Pair
		for _, candidate := range selected {
			if candidate.ID == sc.ID {
				pair = candidate
				break
			}
		}
		if pair.ID == "" {
			t.Fatal("checkpoint case is not present in fixed dataset")
		}
		if err := CompareDecisionCase(t.Context(), checkpoint, pair, legacySensitivity, decisionsSensitivity, legacy, decisions, legacyJudgeReservationNanoUSD, decisionsJudgeReservationNanoUSD); err != nil {
			stage := "comparison_error"
			if errors.Is(err, ErrCheckpointBudget) {
				stage = "budget_exhausted"
			}
			_ = checkpoint.Stop(stage)
			t.Fatalf("comparison stopped at %s; reconcile provider usage before retry", pair.ID)
		}
	}
	if len(checkpoint.Bundle.Sessions) == caseCount {
		if err := checkpoint.Complete(); err != nil {
			t.Fatal(err)
		}
	} else if err := checkpoint.Stop("pilot_complete"); err != nil {
		t.Fatal("pilot checkpoint save failed")
	}
	spent, err := checkpoint.Spent()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("comparison cases=%d total cases=%d measured nano USD=%d", len(checkpoint.Bundle.Sessions), caseCount, spent)
}

type legacySensitivityAdapter struct{ claims.Client }

func (c legacySensitivityAdapter) Classify(ctx context.Context, text string) (claims.SensitivityDecision, error) {
	sensitive, err := c.Client.Sensitive(ctx, text)
	choice := "not_sensitive"
	if sensitive {
		choice = "sensitive"
	}
	return claims.SensitivityDecision{Choice: choice}, err
}

func envInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	v := os.Getenv(name)
	if v == "" && fallback > 0 {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("invalid %s", name)
	}
	return n
}

func readFileFatal(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sameStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

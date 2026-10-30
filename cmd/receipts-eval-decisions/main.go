// receipts-eval-decisions scores a completed Decisions comparison checkpoint offline.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"receipts/internal/evaluation"
)

func main() {
	datasetPath := flag.String("dataset", "testdata/eval/pairs.jsonl", "original immutable labeled corpus")
	progressPath := flag.String("review", "docs/t54-label-review-progress.json", "owner-reviewed label overlay")
	summaryPath := flag.String("review-summary", "docs/evidence/t54-human-reviewed-report.json", "review protocol binding")
	checkpointPath := flag.String("checkpoint", "", "completed Decisions comparison checkpoint")
	scorerPath := flag.String("scorer-source", "internal/evaluation/decision_report.go", "scorer source used for this report")
	reportPath := flag.String("report-out", "", "new report file (exclusive create); empty writes stdout")
	freezePath := flag.String("freeze-out", "", "dev only: write selected-threshold freeze to a new file")
	freezeIn := flag.String("freeze", "", "heldout only: development freeze JSON")
	devReportPath := flag.String("dev-report", "", "heldout only: exact dev report bound by freeze")
	devCheckpointPath := flag.String("dev-checkpoint", "", "heldout only: exact dev checkpoint bound by freeze")
	confidence := flag.Float64("min-confidence", -1, "explicit threshold from dev grid")
	probability := flag.Float64("min-probability", -1, "explicit threshold from dev grid")
	flag.Parse()
	if *checkpointPath == "" {
		fail(errors.New("checkpoint is required"))
	}
	datasetRaw, progressRaw, summaryRaw := read(*datasetPath), read(*progressPath), read(*summaryPath)
	checkpointRaw, scorerRaw := read(*checkpointPath), read(*scorerPath)
	pairs, hashes, err := evaluation.ReadOwnerReviewedPairs(datasetRaw, progressRaw, summaryRaw)
	if err != nil {
		fail(err)
	}
	var bundle evaluation.SessionBundle
	if err = json.Unmarshal(checkpointRaw, &bundle); err != nil {
		fail(err)
	}
	scorerSum := sha256.Sum256(scorerRaw)
	scorerHash := hex.EncodeToString(scorerSum[:])
	var selected *evaluation.DecisionThreshold
	var frozen *evaluation.DecisionFreeze
	switch bundle.Split {
	case "dev":
		if *freezeIn != "" || *devReportPath != "" || *devCheckpointPath != "" || (*confidence >= 0) != (*probability >= 0) || (*freezePath != "" && (*confidence < 0 || *reportPath == "")) {
			fail(errors.New("dev scoring accepts an optional paired threshold and freeze-out, not a freeze-in"))
		}
		if *confidence >= 0 {
			selected = &evaluation.DecisionThreshold{MinConfidence: *confidence, MinProbability: *probability}
		}
	case "heldout":
		if *freezeIn == "" || *devReportPath == "" || *devCheckpointPath == "" || *freezePath != "" || *confidence >= 0 || *probability >= 0 || *reportPath == "" {
			fail(errors.New("heldout scoring requires freeze, dev-report, dev-checkpoint, and report-out; no threshold overrides"))
		}
		freezeRaw := read(*freezeIn)
		devReportRaw, devCheckpointRaw := read(*devReportPath), read(*devCheckpointPath)
		var f evaluation.DecisionFreeze
		if json.Unmarshal(freezeRaw, &f) != nil {
			fail(errors.New("invalid development freeze"))
		}
		devHash, devCheckpointHash := sha256.Sum256(devReportRaw), sha256.Sum256(devCheckpointRaw)
		var devReport evaluation.DecisionComparisonReport
		if json.Unmarshal(devReportRaw, &devReport) != nil || devReport.Split != "dev" || devReport.Protocol != evaluation.DecisionSweepPolicy || devReport.RunStatus != "complete" || devReport.DatasetSHA256 != hashes.DatasetSHA256 || devReport.ReviewSHA256 != hashes.ReviewSHA256 || devReport.CheckpointSHA256 != f.DevCheckpointSHA256 || devReport.Selected == nil || devReport.Selected.Threshold != f.Threshold || devReport.AutomaticStampsEnabled {
			fail(errors.New("development report binding is invalid"))
		}
		if f.Protocol != evaluation.DecisionSweepPolicy || f.DatasetSHA256 != hashes.DatasetSHA256 || f.ReviewSHA256 != hashes.ReviewSHA256 || f.DevReportSHA256 != hex.EncodeToString(devHash[:]) || f.DevCheckpointSHA256 != hex.EncodeToString(devCheckpointHash[:]) || f.AutomaticStampsEnabled || f.ScorerSourceSHA256 != scorerHash || devReport.ScorerSourceSHA256 != scorerHash || !sameStrings(f.Models, bundle.ComparisonModels) || !sameStrings(devReport.Models, bundle.ComparisonModels) || !sameStrings(f.SourceHashes, bundle.SourceHashes) || !sameStrings(devReport.SourceHashes, bundle.SourceHashes) {
			fail(errors.New("development freeze does not bind to this heldout checkpoint and current sources"))
		}
		frozen, selected = &f, &f.Threshold
	default:
		fail(errors.New("checkpoint split must be dev or heldout"))
	}
	report, err := evaluation.ScoreDecisionComparison(pairs, hashes, bundle, checkpointRaw, scorerHash, selected)
	if err != nil {
		fail(err)
	}
	reportRaw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fail(err)
	}
	if bundle.Split == "dev" && selected != nil && *freezePath != "" {
		reportHash := writeExclusive(*reportPath, reportRaw)
		checkpointHash := sha256.Sum256(checkpointRaw)
		freeze := evaluation.DecisionFreeze{Protocol: evaluation.DecisionSweepPolicy, DatasetSHA256: hashes.DatasetSHA256, ReviewSHA256: hashes.ReviewSHA256, DevCheckpointSHA256: hex.EncodeToString(checkpointHash[:]), DevReportSHA256: reportHash, Models: bundle.ComparisonModels, SourceHashes: bundle.SourceHashes, ScorerSourceSHA256: scorerHash, Threshold: *selected, AutomaticStampsEnabled: false}
		freezeRaw, err := json.MarshalIndent(freeze, "", "  ")
		if err != nil {
			fail(err)
		}
		writeExclusive(*freezePath, freezeRaw)
		return
	}
	if frozen != nil {
		// Freeze is required to be single-use for this exact corpus/model/source set.
		report.FreezeSHA256 = hex.EncodeToString(sha256Bytes(read(*freezeIn)))
		reportRaw, err = json.MarshalIndent(report, "", "  ")
		if err != nil {
			fail(err)
		}
	}
	if *reportPath != "" {
		writeExclusive(*reportPath, reportRaw)
	} else {
		fmt.Fprintln(os.Stdout, string(reportRaw))
	}
}

func read(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		fail(err)
	}
	return b
}

func writeExclusive(path string, b []byte) string {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fail(err)
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		fail(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sha256Bytes(b []byte) []byte { sum := sha256.Sum256(b); return sum[:] }

func sameStrings(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

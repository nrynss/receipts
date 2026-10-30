package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"receipts/internal/detect"
	"receipts/internal/judge"
)

const DecisionSweepPolicy = "decisions-choice-threshold-v1"

type DecisionThreshold struct {
	MinConfidence  float64 `json:"min_confidence"`
	MinProbability float64 `json:"min_probability"`
}

type DecisionOverlayHashes struct {
	DatasetSHA256 string
	ReviewSHA256  string
}

type reviewedProgress struct {
	DatasetSHA256 string   `json:"dataset_sha256"`
	Reviewer      string   `json:"reviewer"`
	ApprovedIDs   []string `json:"approved_ids"`
	Disagreements []struct {
		ID               string `json:"id"`
		OriginalLabel    string `json:"original_label"`
		OriginalExpected string `json:"original_expected"`
		ReviewedLabel    string `json:"reviewed_label"`
		ReviewedExpected string `json:"reviewed_expected"`
		Reason           string `json:"reason"`
		Reviewer         string `json:"reviewer"`
	} `json:"disagreements"`
	Complete bool `json:"complete"`
}

type reviewedSummary struct {
	Protocol               string `json:"protocol"`
	DatasetSHA256          string `json:"dataset_sha256"`
	ReviewSHA256           string `json:"review_sha256"`
	LabelCorrections       int    `json:"label_corrections"`
	Policy                 string `json:"policy"`
	AutomaticStampsEnabled bool   `json:"automatic_stamps_enabled"`
}

// ReadOwnerReviewedPairs applies the six owner corrections as a scoring
// overlay. It leaves the original dataset bytes, labels, partitions, and
// evaluation recordings untouched.
func ReadOwnerReviewedPairs(datasetRaw, progressRaw, summaryRaw []byte) ([]Pair, DecisionOverlayHashes, error) {
	pairs, datasetHash, err := ReadPairs(datasetRaw)
	if err != nil {
		return nil, DecisionOverlayHashes{}, err
	}
	reviewHash := sha256.Sum256(progressRaw)
	hash := DecisionOverlayHashes{DatasetSHA256: datasetHash, ReviewSHA256: hex.EncodeToString(reviewHash[:])}
	var progress reviewedProgress
	var summary reviewedSummary
	if json.Unmarshal(progressRaw, &progress) != nil || json.Unmarshal(summaryRaw, &summary) != nil || !progress.Complete || progress.Reviewer == "" || progress.DatasetSHA256 != datasetHash || summary.DatasetSHA256 != datasetHash || summary.ReviewSHA256 != hash.ReviewSHA256 || summary.LabelCorrections != len(progress.Disagreements) || !strings.Contains(summary.Protocol, "Post-collection human label audit") || summary.Policy != "review-only-v1" || summary.AutomaticStampsEnabled {
		return nil, DecisionOverlayHashes{}, errors.New("invalid owner-review overlay binding")
	}
	byID := make(map[string]int, len(pairs))
	for i, p := range pairs {
		if _, exists := byID[p.ID]; exists {
			return nil, DecisionOverlayHashes{}, errors.New("duplicate dataset id")
		}
		byID[p.ID] = i
	}
	approved := make(map[string]bool, len(progress.ApprovedIDs))
	for _, id := range progress.ApprovedIDs {
		if _, exists := byID[id]; !exists || approved[id] {
			return nil, DecisionOverlayHashes{}, errors.New("invalid approved review id")
		}
		approved[id] = true
	}
	corrected := make(map[string]bool, len(progress.Disagreements))
	for _, d := range progress.Disagreements {
		i, exists := byID[d.ID]
		if !exists || corrected[d.ID] || approved[d.ID] || d.Reviewer == "" || d.Reviewer != progress.Reviewer || d.Reason == "" || !validLabel(d.ReviewedLabel) || !validExpected(d.ReviewedExpected) {
			return nil, DecisionOverlayHashes{}, errors.New("invalid label correction")
		}
		if pairs[i].Label != d.OriginalLabel || pairs[i].Expected != d.OriginalExpected {
			return nil, DecisionOverlayHashes{}, fmt.Errorf("review correction no longer matches original label: %s", d.ID)
		}
		pairs[i].Label = d.ReviewedLabel
		pairs[i].Expected = d.ReviewedExpected
		corrected[d.ID] = true
	}
	if len(approved)+len(corrected) != len(pairs) {
		return nil, DecisionOverlayHashes{}, errors.New("owner review does not cover the full dataset")
	}
	for _, p := range pairs {
		if !approved[p.ID] && !corrected[p.ID] {
			return nil, DecisionOverlayHashes{}, errors.New("owner review omits a dataset id")
		}
	}
	return pairs, hash, nil
}

type DecisionThresholdCounts struct {
	Threshold        DecisionThreshold `json:"threshold"`
	Total            int               `json:"total"`
	EligiblePositive int               `json:"eligible_positive"`
	NegativeLabels   int               `json:"negative_labels"`
	Stamps           int               `json:"stamps"`
	CorrectStamps    int               `json:"correct_stamps"`
	FalseStamps      int               `json:"false_stamps"`
	Abstentions      int               `json:"abstentions"`
	Precision        *float64          `json:"precision"`
	Recall           *float64          `json:"recall"`
	FalseStampRate   *float64          `json:"false_stamp_rate"`
	AbstentionRate   *float64          `json:"abstention_rate"`
	FalseStampIDs    []string          `json:"false_stamp_ids"`
}

type DecisionInspection struct {
	ID                    string             `json:"id"`
	Claim                 string             `json:"claim"`
	Later                 string             `json:"later"`
	Label                 string             `json:"label"`
	Expected              string             `json:"expected"`
	Outcome               string             `json:"outcome"`
	SensitivityClaim      string             `json:"sensitivity_claim_choice"`
	SensitivityLater      string             `json:"sensitivity_later_choice"`
	SensitivityConfidence []float64          `json:"sensitivity_confidence,omitempty"`
	Verdict               string             `json:"verdict,omitempty"`
	Confidence            *float64           `json:"confidence,omitempty"`
	Probabilities         map[string]float64 `json:"probabilities,omitempty"`
	FalseStamp            bool               `json:"false_stamp"`
}

type DecisionComparisonReport struct {
	Protocol               string                    `json:"protocol"`
	DatasetSHA256          string                    `json:"dataset_sha256"`
	ReviewSHA256           string                    `json:"review_sha256"`
	CheckpointSHA256       string                    `json:"checkpoint_sha256"`
	FreezeSHA256           string                    `json:"freeze_sha256,omitempty"`
	Split                  string                    `json:"split"`
	RunStatus              string                    `json:"run_status"`
	Models                 map[string]string         `json:"models"`
	SourceHashes           map[string]string         `json:"source_hashes"`
	ScorerSourceSHA256     string                    `json:"scorer_source_sha256"`
	PostCollectionAudit    bool                      `json:"post_collection_audit"`
	AutomaticStampsEnabled bool                      `json:"automatic_stamps_enabled"`
	ThresholdSweep         []DecisionThresholdCounts `json:"threshold_sweep,omitempty"`
	Selected               *DecisionThresholdCounts  `json:"selected,omitempty"`
	Inspections            []DecisionInspection      `json:"inspections,omitempty"`
}

type DecisionFreeze struct {
	Protocol               string            `json:"protocol"`
	DatasetSHA256          string            `json:"dataset_sha256"`
	ReviewSHA256           string            `json:"review_sha256"`
	DevCheckpointSHA256    string            `json:"dev_checkpoint_sha256"`
	DevReportSHA256        string            `json:"dev_report_sha256"`
	Models                 map[string]string `json:"models"`
	SourceHashes           map[string]string `json:"source_hashes"`
	ScorerSourceSHA256     string            `json:"scorer_source_sha256"`
	Threshold              DecisionThreshold `json:"threshold"`
	AutomaticStampsEnabled bool              `json:"automatic_stamps_enabled"`
}

func DecisionThresholdGrid() []DecisionThreshold {
	values := []float64{0, 0.5, 0.6, 0.7, 0.8, 0.9, 1}
	out := make([]DecisionThreshold, 0, len(values)*len(values))
	for _, confidence := range values {
		for _, probability := range values {
			out = append(out, DecisionThreshold{MinConfidence: confidence, MinProbability: probability})
		}
	}
	return out
}

func ScoreDecisionComparison(pairs []Pair, hashes DecisionOverlayHashes, bundle SessionBundle, checkpointRaw []byte, scorerHash string, selected *DecisionThreshold) (DecisionComparisonReport, error) {
	var report DecisionComparisonReport
	if scorerHash == "" || len(checkpointRaw) == 0 || bundle.CorpusKind != "pairs-decisions-comparison" || bundle.DatasetSHA256 != hashes.DatasetSHA256 || (bundle.Split != "dev" && bundle.Split != "heldout") || bundle.Provenance != "live-provider-sanitized" || bundle.RunStatus != "complete" || bundle.JudgeModel != judge.DecisionsModel || len(bundle.Sessions) != bundle.Requested || len(bundle.ComparisonModels) != 4 {
		return report, errors.New("invalid Decisions comparison checkpoint")
	}
	if bundle.Split == "heldout" && selected == nil {
		return report, errors.New("heldout scoring requires a frozen development threshold")
	}
	expectedModels := map[string]string{"legacy_sensitivity": "gpt-4o-mini-2024-07-18", "decisions_sensitivity": judge.DecisionsModel, "legacy_judge": "gpt-4o-mini-2024-07-18", "decisions_judge": judge.DecisionsModel}
	if len(pairs) == 0 || len(bundle.Sessions) != countSplit(pairs, bundle.Split) || !sameModelMap(bundle.ComparisonModels, expectedModels) || len(bundle.SourceHashes) == 0 {
		return report, errors.New("comparison checkpoint does not cover the requested partition")
	}
	checkpointHash := sha256.Sum256(checkpointRaw)
	report = DecisionComparisonReport{Protocol: DecisionSweepPolicy, DatasetSHA256: hashes.DatasetSHA256, ReviewSHA256: hashes.ReviewSHA256, CheckpointSHA256: hex.EncodeToString(checkpointHash[:]), Split: bundle.Split, RunStatus: bundle.RunStatus, Models: cloneStrings(bundle.ComparisonModels), SourceHashes: cloneStrings(bundle.SourceHashes), ScorerSourceSHA256: scorerHash, PostCollectionAudit: true, AutomaticStampsEnabled: false}
	thresholds := DecisionThresholdGrid()
	if selected != nil {
		if !validThreshold(*selected) {
			return report, errors.New("invalid selected decision threshold")
		}
		found := false
		for _, threshold := range thresholds {
			if threshold == *selected {
				found = true
			}
		}
		if !found {
			return report, errors.New("selected thresholds must come from the development sweep grid")
		}
		if bundle.Split == "heldout" {
			thresholds = []DecisionThreshold{*selected}
		}
	}
	records := make(map[string]SessionRecordedCase, len(bundle.Sessions))
	for _, record := range bundle.Sessions {
		if record.ID == "" || record.ErrorStage != "" {
			return report, errors.New("incomplete comparison case")
		}
		if _, exists := records[record.ID]; exists {
			return report, errors.New("duplicate comparison case")
		}
		records[record.ID] = record
	}
	for _, threshold := range thresholds {
		counts := DecisionThresholdCounts{Threshold: threshold, FalseStampIDs: []string{}}
		var inspections []DecisionInspection
		for _, pair := range pairs {
			if pair.Split != bundle.Split {
				continue
			}
			record, exists := records[pair.ID]
			if !exists {
				return report, fmt.Errorf("missing comparison case %s", pair.ID)
			}
			inspection, err := inspectDecisionCase(pair, record, threshold)
			if err != nil {
				return report, fmt.Errorf("%s: %w", pair.ID, err)
			}
			counts.Total++
			positive := pair.Label == "contradiction" && pair.Expected == "stamp"
			if positive {
				counts.EligiblePositive++
			} else {
				counts.NegativeLabels++
			}
			if inspection.Outcome == "stamp" {
				counts.Stamps++
				if positive {
					counts.CorrectStamps++
				} else {
					counts.FalseStamps++
					counts.FalseStampIDs = append(counts.FalseStampIDs, pair.ID)
					inspection.FalseStamp = true
				}
			}
			if inspection.Outcome == "review" || inspection.Outcome == "suppressed" {
				counts.Abstentions++
			}
			if selected != nil && threshold == *selected {
				inspections = append(inspections, inspection)
			}
		}
		counts.Precision = ratio(counts.CorrectStamps, counts.Stamps)
		counts.Recall = ratio(counts.CorrectStamps, counts.EligiblePositive)
		counts.FalseStampRate = ratio(counts.FalseStamps, counts.NegativeLabels)
		counts.AbstentionRate = ratio(counts.Abstentions, counts.Total)
		if bundle.Split == "dev" {
			report.ThresholdSweep = append(report.ThresholdSweep, counts)
		}
		if selected != nil && threshold == *selected {
			report.Selected = &counts
			report.Inspections = inspections
		}
	}
	if len(records) != countSplit(pairs, bundle.Split) {
		return report, errors.New("comparison checkpoint includes unknown case ids")
	}
	return report, nil
}

func inspectDecisionCase(pair Pair, record SessionRecordedCase, threshold DecisionThreshold) (DecisionInspection, error) {
	events := map[string]SessionEvent{}
	for _, event := range record.Trace.Events {
		if event.State != "complete" || !event.Measured || event.Failed || event.RequestSHA256 == "" || event.InputSHA256 == "" {
			return DecisionInspection{}, errors.New("unmeasured or unresolved event")
		}
		if _, exists := events[event.Kind]; exists {
			return DecisionInspection{}, errors.New("duplicate comparison event")
		}
		events[event.Kind] = event
	}
	need := []string{"sensitive-comparison-legacy-claim", "sensitive-comparison-decisions-claim", "sensitive-comparison-legacy-later", "sensitive-comparison-decisions-later"}
	for _, kind := range need {
		if _, ok := events[kind]; !ok {
			return DecisionInspection{}, errors.New("missing sensitivity event")
		}
	}
	claimSens := events["sensitive-comparison-decisions-claim"]
	laterSens := events["sensitive-comparison-decisions-later"]
	if !validSensitivityEvent(claimSens) || !validSensitivityEvent(laterSens) {
		return DecisionInspection{}, errors.New("invalid Decisions sensitivity scores")
	}
	inspection := DecisionInspection{ID: pair.ID, Claim: pair.Claim, Later: pair.Later, Label: pair.Label, Expected: pair.Expected, SensitivityClaim: claimSens.Choice, SensitivityLater: laterSens.Choice, SensitivityConfidence: []float64{*claimSens.Confidence, *laterSens.Confidence}}
	if claimSens.Choice != "not_sensitive" || laterSens.Choice != "not_sensitive" || claimSens.Probabilities["not_sensitive"] < threshold.MinProbability || laterSens.Probabilities["not_sensitive"] < threshold.MinProbability || *claimSens.Confidence < threshold.MinConfidence || *laterSens.Confidence < threshold.MinConfidence {
		inspection.Outcome = "suppressed"
		return inspection, nil
	}
	legacySensClaim := events["sensitive-comparison-legacy-claim"]
	legacySensLater := events["sensitive-comparison-legacy-later"]
	if legacySensClaim.Choice != "not_sensitive" || legacySensLater.Choice != "not_sensitive" {
		inspection.Outcome = "suppressed"
		return inspection, nil
	}
	legacy, hasLegacy := events["judge-comparison-legacy"]
	decision, hasDecision := events["judge-comparison-decisions"]
	if !hasLegacy || !hasDecision || legacy.Judgment.Verdict == "" || decision.Judgment.Verdict == "" {
		return DecisionInspection{}, errors.New("missing paired judge events for a nonsensitive case")
	}
	if decision.Judgment.Confidence == nil || !validChoiceProbabilities(decision.Judgment.Probabilities) || decision.Judgment.ClaimSourceID != pair.ID+":claim" || decision.Judgment.LaterSourceID != pair.ID+":later" {
		return DecisionInspection{}, errors.New("invalid Decisions judge scores")
	}
	inspection.Verdict = string(decision.Judgment.Verdict)
	inspection.Confidence = decision.Judgment.Confidence
	inspection.Probabilities = cloneFloats(decision.Judgment.Probabilities)
	choiceProbability := decision.Judgment.Probabilities[inspection.Verdict]
	if choiceProbability < threshold.MinProbability || *decision.Judgment.Confidence < threshold.MinConfidence {
		inspection.Outcome = "review"
	} else if decision.Judgment.Verdict == detect.Contradicts {
		inspection.Outcome = "stamp"
	} else if decision.Judgment.Verdict == detect.DoesNot {
		inspection.Outcome = "quiet"
	} else {
		inspection.Outcome = "review"
	}
	return inspection, nil
}

func validSensitivityEvent(e SessionEvent) bool {
	return e.Model == judge.DecisionsModel && e.Confidence != nil && unitInterval(*e.Confidence) && (e.Choice == "sensitive" || e.Choice == "not_sensitive" || e.Choice == "unsure") && validProbabilities(e.Probabilities, []string{"sensitive", "not_sensitive", "unsure"})
}

func validChoiceProbabilities(p map[string]float64) bool {
	return validProbabilities(p, []string{"contradicts", "does_not_contradict", "unsure"})
}

func validProbabilities(p map[string]float64, values []string) bool {
	if len(p) != len(values) {
		return false
	}
	sum := 0.0
	for _, v := range values {
		x, ok := p[v]
		if !ok || !unitInterval(x) {
			return false
		}
		sum += x
	}
	return math.Abs(sum-1) <= 0.03
}

func validThreshold(t DecisionThreshold) bool {
	return unitInterval(t.MinConfidence) && unitInterval(t.MinProbability)
}

func unitInterval(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 }

func validLabel(v string) bool {
	return v == "contradiction" || v == "no_contradiction" || v == "review" || v == "suppress"
}
func validExpected(v string) bool { return v == "stamp" || v == "quiet" || v == "review" }

func countSplit(pairs []Pair, split string) int {
	n := 0
	for _, p := range pairs {
		if p.Split == split {
			n++
		}
	}
	return n
}

func cloneStrings(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneFloats(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sameModelMap(a, b map[string]string) bool {
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

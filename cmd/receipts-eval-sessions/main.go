// receipts-eval-sessions replays sanitized session traces offline.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"receipts/internal/evaluation"
	"receipts/internal/judge"
	"receipts/internal/retrieval"
)

func fail(e error)           { fmt.Fprintln(os.Stderr, e); os.Exit(1) }
func hash(raw []byte) string { x := sha256.Sum256(raw); return hex.EncodeToString(x[:]) }
func same(a, b []string) bool {
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	return reflect.DeepEqual(a, b)
}
func main() {
	dataset := flag.String("dataset", "testdata/eval/sessions.json", "session corpus")
	recording := flag.String("recording", "", "recorded bundle")
	review := flag.String("review", "", "independent human review assertion")
	freezeFile := flag.String("freeze", "", "policy freeze manifest required for heldout")
	flag.Parse()
	if *recording == "" {
		fail(errors.New("recording required"))
	}
	raw, e := os.ReadFile(*dataset)
	if e != nil {
		fail(e)
	}

	data, e := os.ReadFile(*recording)
	if e != nil {
		fail(e)
	}
	var bundle evaluation.SessionBundle
	if e = json.Unmarshal(data, &bundle); e != nil {
		fail(e)
	}
	var cases []evaluation.SessionCase
	labels := map[string]evaluation.Pair{}
	if bundle.CorpusKind == "pairs" {
		pairs, _, err := evaluation.ReadPairs(raw)
		if err != nil {
			fail(err)
		}
		for _, p := range pairs {
			if p.Split == bundle.Split {
				labels[p.ID] = p
			}
		}
		cases, err = evaluation.PairSessions(pairs, bundle.Split)
		if err != nil {
			fail(err)
		}
	} else if bundle.CorpusKind == "sessions" {
		var corpus struct {
			Sessions []evaluation.SessionCase `json:"sessions"`
		}
		if err := json.Unmarshal(raw, &corpus); err != nil {
			fail(err)
		}
		cases = corpus.Sessions
	} else {
		fail(errors.New("invalid corpus kind"))
	}
	if bundle.DatasetSHA256 != hash(raw) || (bundle.Split != "dev" && bundle.Split != "heldout") || bundle.Provenance != "live-provider-sanitized" || bundle.JudgeModel != judge.Model || bundle.EmbeddingModel != retrieval.DefaultModel {
		fail(errors.New("invalid manifest"))
	}
	if bundle.Split == "heldout" {
		if *freezeFile == "" {
			fail(errors.New("heldout requires freeze"))
		}
		b, e := os.ReadFile(*freezeFile)
		if e != nil {
			fail(e)
		}
		var f evaluation.Freeze
		if json.Unmarshal(b, &f) != nil || hash(b) != bundle.FreezeSHA256 || f.DatasetSHA256 != bundle.DatasetSHA256 || f.Policy != "review-only-v1" || f.DevReportSHA256 == "" || f.JudgeSourceSHA256 != bundle.SourceHashes["../judge/openai.go"] || f.PolicySourceSHA256 != bundle.SourceHashes["../intelligence/policy.go"] {
			fail(errors.New("invalid policy freeze"))
		}
	}
	paths := []string{"../judge/openai.go", "../claims/openai.go", "../claims/sensitive.go", "../retrieval/openai.go", "../retrieval/retrieval.go", "../intelligence/service.go", "../intelligence/policy.go", "../providerusage/usage.go", "../commands/receipt.go", "../store/detect.go", "../store/embeddings.go", "../store/live_transcript.go", "../store/store.go", "session.go", "session_bundle.go", "trace.go", "../../cmd/receipts-eval-sessions/main.go"}
	if len(bundle.SourceHashes) != len(paths) {
		fail(errors.New("invalid source manifest"))
	}
	for _, p := range paths {
		b, e := os.ReadFile(filepath.Join("internal/evaluation", p))
		if e != nil {
			fail(e)
		}
		if hash(b) != bundle.SourceHashes[p] {
			fail(errors.New("source hash mismatch: " + p))
		}
	}
	reviewed := false
	if *review != "" {
		b, e := os.ReadFile(*review)
		if e != nil {
			fail(e)
		}
		var r evaluation.Review
		if e = json.Unmarshal(b, &r); e != nil {
			fail(e)
		}
		if r.DatasetSHA256 != bundle.DatasetSHA256 || r.Reviewer == "" || r.Decision != "independent-human-reviewed" {
			fail(errors.New("invalid review assertion"))
		}
		reviewed = true
	}
	records := map[string]evaluation.SessionRecordedCase{}
	for _, r := range bundle.Sessions {
		if _, ok := records[r.ID]; ok {
			fail(errors.New("duplicate recording"))
		}
		records[r.ID] = r
	}
	type caseReport struct {
		ID           string                   `json:"id"`
		Result       evaluation.SessionResult `json:"result"`
		LatencyMS    int64                    `json:"latency_ms"`
		SpendNanoUSD int64                    `json:"spend_nano_usd"`
		ErrorStage   string                   `json:"error_stage,omitempty"`
	}
	out := struct {
		DatasetSHA256, Split, Policy          string
		AccuracyReviewed                      bool
		Cases                                 []caseReport
		Total, SourceRejected, Stamps, Errors int
		RetrievalAttempts, RetrievalMisses    *int
		LatencyMS, SpendNanoUSD               int64
		TargetMetrics                         *targetMetrics `json:"target_metrics,omitempty"`
	}{DatasetSHA256: bundle.DatasetSHA256, Split: bundle.Split, Policy: "review-only-v1", AccuracyReviewed: reviewed}
	attempts, misses := 0, 0
	metrics := targetMetrics{}
	for _, sc := range cases {
		if sc.Split != bundle.Split {
			continue
		}
		r, ok := records[sc.ID]
		if !ok {
			fail(fmt.Errorf("missing recording %s", sc.ID))
		}
		delete(records, sc.ID)
		if r.ID != r.Trace.SessionID || r.LatencyMS < 0 {
			fail(errors.New("invalid case"))
		}
		c := caseReport{ID: sc.ID, LatencyMS: r.LatencyMS, ErrorStage: r.ErrorStage}
		out.Total++
		if reviewed && bundle.CorpusKind == "pairs" {
			p := labels[sc.ID]
			if p.Label == "contradiction" && p.Expected == "stamp" {
				metrics.EligibleContradictions++
			} else {
				metrics.NegativeLabels++
			}
		}
		out.LatencyMS += r.LatencyMS
		for _, ev := range r.Trace.Events {
			if ev.SpendNanoUSD < 0 {
				fail(errors.New("invalid usage"))
			}
			if r.ErrorStage == "" {
				b, e := hex.DecodeString(ev.RequestSHA256)
				if e != nil || len(b) != 32 {
					fail(errors.New("missing request digest"))
				}
			}
			if ev.Measured {
				c.SpendNanoUSD += ev.SpendNanoUSD
			}
		}
		out.SpendNanoUSD += c.SpendNanoUSD
		if r.ErrorStage != "" {
			out.Errors++
			if reviewed && bundle.CorpusKind == "pairs" {
				metrics.Abstentions++
			}
			out.Cases = append(out.Cases, c)
			continue
		}
		dir, e := os.MkdirTemp("", "receipts-eval-session-")
		if e != nil {
			fail(e)
		}
		adapters, check := evaluation.TraceReplay(r.Trace, retrieval.DefaultDimensions, 500000000)
		result, e := evaluation.RunSession(context.Background(), dir, sc, adapters)
		os.RemoveAll(dir)
		if e != nil {
			fail(e)
		}
		if e = check(); e != nil {
			fail(e)
		}
		if !same(result.Captured, r.Result.Captured) || !same(result.Judged, r.Result.Judged) || !reflect.DeepEqual(result.Outcomes, r.Result.Outcomes) || result.SourceRejected != r.Result.SourceRejected || result.TargetRetrieved != r.Result.TargetRetrieved {
			fail(fmt.Errorf("replayed outcome mismatch: %s", sc.ID))
		}
		if result.SourceRejected {
			out.SourceRejected++
		} else {
			targetCaptured := false
			for _, id := range result.Captured {
				if id == sc.TargetID {
					targetCaptured = true
				}
			}
			if targetCaptured {
				attempts++
				if !result.TargetRetrieved {
					misses++
				}
			}
		}
		for _, v := range result.Outcomes {
			if v == "stamp" {
				out.Stamps++
			}
		}
		if reviewed && bundle.CorpusKind == "pairs" {
			outcome := result.Outcomes[sc.TargetID]
			p := labels[sc.ID]
			if outcome == "stamp" {
				metrics.Stamps++
				if p.Label == "contradiction" && p.Expected == "stamp" {
					metrics.CorrectStamps++
				} else {
					metrics.FalseStamps++
					metrics.FalseStampIDs = append(metrics.FalseStampIDs, sc.ID)
				}
			} else if outcome != "dismissed" {
				metrics.Abstentions++
			}
		}
		if !reviewed {
			result.TargetRetrieved = false
		}
		c.Result = result
		out.Cases = append(out.Cases, c)
	}
	if len(records) != 0 {
		fail(errors.New("extra recording"))
	}
	if reviewed {
		out.RetrievalAttempts = &attempts
		out.RetrievalMisses = &misses
		if bundle.CorpusKind == "pairs" {
			metrics.Precision = ratio(metrics.CorrectStamps, metrics.Stamps)
			metrics.Recall = ratio(metrics.CorrectStamps, metrics.EligibleContradictions)
			metrics.FalseStampRate = ratio(metrics.FalseStamps, metrics.NegativeLabels)
			metrics.AbstentionRate = ratio(metrics.Abstentions, out.Total)
			out.TargetMetrics = &metrics
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if e = enc.Encode(out); e != nil {
		fail(e)
	}
}

// Metrics apply to the designated target only; distractor verdicts are unlabelled.
type targetMetrics struct {
	EligibleContradictions, NegativeLabels, Stamps, CorrectStamps, FalseStamps, Abstentions int
	FalseStampIDs                                                                           []string
	Precision, Recall, FalseStampRate, AbstentionRate                                       *float64
}

func ratio(n, d int) *float64 {
	if d == 0 {
		return nil
	}
	v := float64(n) / float64(d)
	return &v
}

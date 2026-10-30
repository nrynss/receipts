// Package evaluation replays recorded model outputs without making provider calls.
package evaluation

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"receipts/internal/detect"
	"receipts/internal/intelligence"
	"receipts/internal/receipt"
	"receipts/internal/retrieval"
)

// Source hashes freeze this policy experiment. Any source change requires a new
// explicitly reviewed experiment version, including the held-out decision.
const JudgeSourceHash = "e444df7a8d94d5ef0117107e58ddac0c8271ca619cc7a67ef87cb6b48dcb8022"
const RetrievalSourceHash = "c6c2c50d6f39aadfb1783458ea3f86c6275bdcb6a51cbb8d2b0e72e39e92402c"
const PolicySourceHash = "26cd2090e77c7e3bdbd4ec86af0485db026ed6ab065c6c72c8d9cadadd35a09b"

// Pair labels are used only by Report, never as model input.
type Pair struct {
	ID       string `json:"id"`
	Split    string `json:"split"`
	Claim    string `json:"claim"`
	Later    string `json:"later"`
	Label    string `json:"label"`
	Expected string `json:"expected"`
}

// Recording is a sanitized provider result for one case. All errors are stage
// codes, not provider bodies; missing and ambiguous usage fails closed.
type Recording struct {
	ID             string         `json:"id"`
	ClaimVector    []float64      `json:"claim_vector,omitempty"`
	LaterVector    []float64      `json:"later_vector,omitempty"`
	ClaimSensitive bool           `json:"claim_sensitive"`
	LaterSensitive bool           `json:"later_sensitive"`
	Verdict        detect.Verdict `json:"verdict,omitempty"`
	ClaimSourceID  string         `json:"claim_source_id,omitempty"`
	LaterSourceID  string         `json:"later_source_id,omitempty"`
	ErrorStage     string         `json:"error_stage,omitempty"`
	LatencyMS      int64          `json:"latency_ms"`
	SpendNanoUSD   int64          `json:"spend_nano_usd"`
	UsageMeasured  bool           `json:"usage_measured"`
}
type Manifest struct {
	DatasetSHA256         string `json:"dataset_sha256"`
	Split                 string `json:"split"`
	Provenance            string `json:"provenance"`
	JudgeModel            string `json:"judge_model"`
	EmbeddingModel        string `json:"embedding_model"`
	Policy                string `json:"policy"`
	JudgeSourceSHA256     string `json:"judge_source_sha256"`
	RetrievalSourceSHA256 string `json:"retrieval_source_sha256"`
	PolicySourceSHA256    string `json:"policy_source_sha256"`
}

// Review attests independent human review of the exact labelled corpus. The
// program cannot authenticate the reviewer; this is an auditable assertion,
// not a cryptographic identity check. No marker is supplied by this project.
type Review struct {
	DatasetSHA256 string `json:"dataset_sha256"`
	Reviewer      string `json:"reviewer"`
	Decision      string `json:"decision"`
}
type Bundle struct {
	Manifest Manifest    `json:"manifest"`
	Records  []Recording `json:"records"`
}
type CaseResult struct {
	ID            string `json:"id"`
	Outcome       string `json:"outcome"`
	ErrorStage    string `json:"error_stage,omitempty"`
	RetrievalMiss bool   `json:"retrieval_miss"`
	FalseStamp    bool   `json:"false_stamp"`
}
type Summary struct {
	Manifest                                                                                                                                   Manifest     `json:"manifest"`
	Cases                                                                                                                                      []CaseResult `json:"cases"`
	AccuracyReviewed                                                                                                                           bool         `json:"accuracy_reviewed"`
	Total, EligibleContradictions, NegativeLabels, Stamps, CorrectStamps, FalseStamps, Abstentions, RetrievalAttempts, RetrievalMisses, Errors int
	LatencyMS, SpendNanoUSD                                                                                                                    int64
	Precision, Recall, FalseStampRate, AbstentionRate                                                                                          *float64
}

func ratio(n, d int) *float64 {
	if d == 0 {
		return nil
	}
	x := float64(n) / float64(d)
	return &x
}
func ReadPairs(raw []byte) ([]Pair, string, error) {
	sum := sha256.Sum256(raw)
	var ps []Pair
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytesReader(raw))
	for sc.Scan() {
		var p Pair
		if e := json.Unmarshal(sc.Bytes(), &p); e != nil {
			return nil, "", e
		}
		if p.ID == "" || seen[p.ID] || (p.Split != "dev" && p.Split != "heldout") || p.Claim == "" || p.Later == "" {
			return nil, "", errors.New("invalid corpus")
		}
		seen[p.ID] = true
		ps = append(ps, p)
	}
	return ps, hex.EncodeToString(sum[:]), sc.Err()
}

// bytesReader is deliberately local so parsing never changes the hashed bytes.
func bytesReader(b []byte) io.Reader { return &reader{b: b} }

type reader struct{ b []byte }

func (r *reader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}
func Report(pairs []Pair, datasetHash string, b Bundle) (Summary, error) {
	return report(pairs, datasetHash, b, false)
}

// AccuracyReport requires an independently supplied review assertion bound to
// the exact corpus. A self-asserted marker cannot prove a review occurred.
func AccuracyReport(pairs []Pair, datasetHash string, b Bundle, review Review) (Summary, error) {
	if review.DatasetSHA256 != datasetHash || review.Reviewer == "" || review.Decision != "independent-human-reviewed" {
		return Summary{}, errors.New("independent corpus review required")
	}
	return report(pairs, datasetHash, b, true)
}
func report(pairs []Pair, datasetHash string, b Bundle, reviewed bool) (Summary, error) {
	m := b.Manifest
	out := Summary{Manifest: m, AccuracyReviewed: reviewed}
	if m.DatasetSHA256 != datasetHash || (m.Split != "dev" && m.Split != "heldout") || m.Policy != "review-only-v1" || m.JudgeModel != "gpt-4o-mini-2024-07-18" || m.EmbeddingModel != "text-embedding-3-small" || m.Provenance == "" || m.JudgeSourceSHA256 != JudgeSourceHash || m.RetrievalSourceSHA256 != RetrievalSourceHash || m.PolicySourceSHA256 != PolicySourceHash {
		return out, errors.New("invalid manifest")
	}
	records := map[string]Recording{}
	for _, r := range b.Records {
		if r.ID == "" {
			return out, errors.New("empty recording id")
		}
		if _, ok := records[r.ID]; ok {
			return out, errors.New("duplicate recording")
		}
		records[r.ID] = r
	}
	for _, p := range pairs {
		if p.Split != m.Split {
			continue
		}
		out.Total++
		if p.Label == "contradiction" && p.Expected == "stamp" {
			out.EligibleContradictions++
		} else {
			out.NegativeLabels++
		}
		r, ok := records[p.ID]
		if !ok {
			return out, fmt.Errorf("missing recording %s", p.ID)
		}
		delete(records, p.ID)
		c := CaseResult{ID: p.ID}
		if r.LatencyMS < 0 || r.SpendNanoUSD < 0 {
			return out, errors.New("invalid measurement")
		}
		out.LatencyMS += r.LatencyMS
		out.SpendNanoUSD += r.SpendNanoUSD
		if r.ErrorStage != "" || !r.UsageMeasured {
			c.Outcome = "error"
			c.ErrorStage = r.ErrorStage
			if c.ErrorStage == "" {
				c.ErrorStage = "usage"
			}
			out.Errors++
			out.Abstentions++
		} else if r.ClaimSensitive || r.LaterSensitive {
			c.Outcome = "suppressed"
			out.Abstentions++
		} else {
			space := retrieval.Space{Model: m.EmbeddingModel, Version: "evaluation-v1", Dimensions: len(r.ClaimVector)}
			entries := []retrieval.Entry{{Receipt: receipt.Receipt{ID: p.ID, Claim: p.Claim}, Vector: r.ClaimVector}}
			matches, e := retrieval.Nearest(r.LaterVector, space, entries, 3)
			if e != nil {
				return out, fmt.Errorf("%s: invalid vectors", p.ID)
			}
			out.RetrievalAttempts++
			if len(matches) == 0 || matches[0].Receipt.ID != p.ID {
				c.RetrievalMiss = true
				out.RetrievalMisses++
				c.Outcome = "retrieval_miss"
				out.Abstentions++
			} else if r.ClaimSourceID != p.ID+":claim" || r.LaterSourceID != p.ID+":later" {
				return out, fmt.Errorf("%s: invalid source references", p.ID)
			} else {
				o, e := intelligence.JudgmentOutcome(r.Verdict)
				if e != nil {
					return out, e
				}
				c.Outcome = string(o)
				if o == receipt.NeedsReview {
					out.Abstentions++
				}
				if o == receipt.Stamp {
					out.Stamps++
					if p.Label == "contradiction" && p.Expected == "stamp" {
						out.CorrectStamps++
					} else {
						out.FalseStamps++
						c.FalseStamp = true
					}
				}
			}
		}
		out.Cases = append(out.Cases, c)
	}
	if len(records) != 0 {
		return out, errors.New("recording outside requested partition")
	}
	if reviewed {
		out.Precision = ratio(out.CorrectStamps, out.Stamps)
		out.Recall = ratio(out.CorrectStamps, out.EligibleContradictions)
		out.FalseStampRate = ratio(out.FalseStamps, out.NegativeLabels)
	} else {
		// Routing reports contain no label-derived accuracy counts or per-case errors.
		out.EligibleContradictions = 0
		out.NegativeLabels = 0
		out.CorrectStamps = 0
		out.FalseStamps = 0
		for i := range out.Cases {
			out.Cases[i].FalseStamp = false
		}
	}
	out.AbstentionRate = ratio(out.Abstentions, out.Total)
	return out, nil
}

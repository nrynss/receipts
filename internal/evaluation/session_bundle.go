package evaluation

import "errors"

// SessionBundle contains sanitized recorded adapter events for one fixed split.
// Freeze records the policy selected before any held-out collection.
type Freeze struct {
	DatasetSHA256      string `json:"dataset_sha256"`
	Policy             string `json:"policy"`
	DevReportSHA256    string `json:"dev_report_sha256"`
	JudgeSourceSHA256  string `json:"judge_source_sha256"`
	PolicySourceSHA256 string `json:"policy_source_sha256"`
}

type SessionBundle struct {
	Requested      int                   `json:"requested,omitempty"`
	BudgetNanoUSD  int64                 `json:"budget_nano_usd,omitempty"`
	RunStatus      string                `json:"run_status,omitempty"`
	FreezeSHA256   string                `json:"freeze_sha256,omitempty"`
	CorpusKind     string                `json:"corpus_kind"`
	DatasetSHA256  string                `json:"dataset_sha256"`
	Split          string                `json:"split"`
	Provenance     string                `json:"provenance"`
	JudgeModel     string                `json:"judge_model"`
	EmbeddingModel string                `json:"embedding_model"`
	SourceHashes   map[string]string     `json:"source_hashes"`
	Sessions       []SessionRecordedCase `json:"sessions"`
}
type SessionRecordedCase struct {
	LedgerDirectories []string      `json:"ledger_directories,omitempty"`
	ID                string        `json:"id"`
	Trace             SessionTrace  `json:"trace"`
	Result            SessionResult `json:"result"`
	LatencyMS         int64         `json:"latency_ms"`
	ErrorStage        string        `json:"error_stage,omitempty"`
}

// PairSessions creates deterministic synthetic sessions using only text, IDs
// and split, never labels. Distractors come from the same split and distinct
// claims. The resulting chronology and speaker attestation are experimental.
func PairSessions(pairs []Pair, split string) ([]SessionCase, error) {
	var selected []Pair
	for _, p := range pairs {
		if p.Split == split {
			selected = append(selected, p)
		}
	}
	if split != "dev" && split != "heldout" {
		return nil, errors.New("invalid split")
	}
	var result []SessionCase
	for i, p := range selected {
		s := SessionCase{ID: p.ID, Split: split, LaterID: p.ID + ":later", Later: p.Later, LaterAttribution: "broadcaster", TargetID: p.ID}
		seen := map[string]bool{}
		for step := 0; step < len(selected) && len(s.Candidates) < 4; step++ {
			c := selected[(i+step)%len(selected)]
			if seen[c.Claim] {
				continue
			}
			seen[c.Claim] = true
			s.Candidates = append(s.Candidates, SessionCandidate{ID: c.ID, Claim: c.Claim, Attribution: "broadcaster", BeforeLater: true})
		}
		if len(s.Candidates) < 4 {
			return nil, errors.New("not enough distinct claims")
		}
		result = append(result, s)
	}
	return result, nil
}

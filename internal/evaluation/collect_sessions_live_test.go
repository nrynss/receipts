//go:build live

package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"receipts/internal/claims"
	"receipts/internal/judge"
	"receipts/internal/retrieval"
)

// TestCollectSessionsLive performs real paid calls only after explicit budget coordination.
func TestCollectSessionsLive(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY required")
	}
	split := os.Getenv("RECEIPTS_EVAL_SPLIT")
	if split != "dev" && split != "heldout" {
		t.Fatal("invalid split")
	}
	n, err := strconv.Atoi(os.Getenv("RECEIPTS_EVAL_CASES"))
	if err != nil || n < 1 || n > 34 {
		t.Fatal("case count must be 1..34")
	}
	limit, err := strconv.ParseInt(os.Getenv("RECEIPTS_EVAL_BUDGET_NANO_USD"), 10, 64)
	if err != nil || limit < 500000000 || limit > 17000000000 {
		t.Fatal("budget must be 500000000..17000000000 nano USD")
	}
	path := os.Getenv("RECEIPTS_EVAL_OUTPUT")
	if path == "" {
		t.Fatal("output required")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	readHash := func(path string) ([]byte, string) {
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		h := sha256.Sum256(raw)
		return raw, hex.EncodeToString(h[:])
	}
	kind := os.Getenv("RECEIPTS_EVAL_CORPUS")
	if kind != "pairs" && kind != "sessions" {
		t.Fatal("set RECEIPTS_EVAL_CORPUS")
	}
	pathDataset := "../../testdata/eval/sessions.json"
	if kind == "pairs" {
		pathDataset = "../../testdata/eval/pairs.jsonl"
	}
	raw, hash := readHash(pathDataset)
	var cases []SessionCase
	if kind == "pairs" {
		pairs, _, e := ReadPairs(raw)
		if e != nil {
			t.Fatal(e)
		}
		cases, e = PairSessions(pairs, split)
		if e != nil {
			t.Fatal(e)
		}
	} else {
		var corpus struct {
			Sessions []SessionCase `json:"sessions"`
		}
		if e := json.Unmarshal(raw, &corpus); e != nil {
			t.Fatal(e)
		}
		cases = corpus.Sessions
	}

	sources := map[string]string{}
	for _, p := range []string{"../judge/openai.go", "../claims/openai.go", "../claims/sensitive.go", "../retrieval/openai.go", "../retrieval/retrieval.go", "../intelligence/service.go", "../intelligence/policy.go", "../providerusage/usage.go", "../commands/receipt.go", "../store/detect.go", "../store/embeddings.go", "../store/live_transcript.go", "../store/store.go", "session.go", "session_bundle.go", "trace.go", "../../cmd/receipts-eval-sessions/main.go"} {
		_, sources[p] = readHash(p)
	}
	bundle := SessionBundle{CorpusKind: kind, DatasetSHA256: hash, Split: split, Provenance: "live-provider-sanitized", JudgeModel: judge.Model, EmbeddingModel: retrieval.DefaultModel, SourceHashes: sources}
	if split == "heldout" {
		freezePath := os.Getenv("RECEIPTS_EVAL_FREEZE")
		reportPath := os.Getenv("RECEIPTS_EVAL_DEV_REPORT")
		if freezePath == "" || reportPath == "" {
			t.Fatal("held-out requires freeze and development report")
		}
		freezeRaw, freezeHash := readHash(freezePath)
		reportRaw, reportHash := readHash(reportPath)
		var freeze Freeze
		var dev struct {
			DatasetSHA256, Split, Policy string
			Total, Errors                int
		}
		devCount := 0
		if kind == "pairs" {
			pairs, _, e := ReadPairs(raw)
			if e != nil {
				t.Fatal(e)
			}
			for _, p := range pairs {
				if p.Split == "dev" {
					devCount++
				}
			}
		} else {
			for _, sc := range cases {
				if sc.Split == "dev" {
					devCount++
				}
			}
		}
		if json.Unmarshal(freezeRaw, &freeze) != nil || json.Unmarshal(reportRaw, &dev) != nil || freeze.DatasetSHA256 != hash || freeze.Policy != "review-only-v1" || freeze.DevReportSHA256 != reportHash || freeze.JudgeSourceSHA256 != sources["../judge/openai.go"] || freeze.PolicySourceSHA256 != sources["../intelligence/policy.go"] || dev.DatasetSHA256 != hash || dev.Split != "dev" || dev.Policy != "review-only-v1" || dev.Total != devCount || dev.Errors != 0 {
			t.Fatal("invalid policy freeze")
		}
		bundle.FreezeSHA256 = freezeHash
	}

	defer func() {
		if e := json.NewEncoder(f).Encode(bundle); e != nil {
			t.Errorf("write failed: %v", e)
		}
	}()
	spent := int64(0)
	fillSkipped := func() {
		seen := map[string]bool{}
		for _, r := range bundle.Sessions {
			seen[r.ID] = true
		}
		for _, sc := range cases {
			if sc.Split != split || seen[sc.ID] {
				continue
			}
			if len(bundle.Sessions) >= n {
				break
			}
			bundle.Sessions = append(bundle.Sessions, SessionRecordedCase{ID: sc.ID, Trace: SessionTrace{SessionID: sc.ID}, ErrorStage: "not_collected"})
		}
	}
	httpClient := &http.Client{Transport: DigestTransport{}}
	client := claims.Client{APIKey: key, HTTP: httpClient}
	embed := retrieval.Client{APIKey: key, HTTP: httpClient, Space: retrieval.Space{Model: retrieval.DefaultModel, Version: "evaluation-v1", Dimensions: retrieval.DefaultDimensions}}
	j := judge.Client{APIKey: key, HTTP: httpClient}
	for _, sc := range cases {
		if sc.Split != split {
			continue
		}
		if len(bundle.Sessions) >= n {
			break
		}
		if limit-spent < 500000000 {
			fillSkipped()
			t.Error("budget insufficient for next reservation; stopped")
			return
		}
		trace := SessionTrace{SessionID: sc.ID}
		adapters := TraceAdapters(SessionAdapters{Extractor: client, Sensitive: client, Embedder: embed, Judge: j, Dimensions: retrieval.DefaultDimensions, Budget: cost.Price(500000000)}, &trace)
		begin := time.Now()
		result, e := RunSession(context.Background(), t.TempDir(), sc, adapters)
		r := SessionRecordedCase{ID: sc.ID, Trace: trace, Result: result, LatencyMS: time.Since(begin).Milliseconds()}
		for _, ev := range trace.Events {
			if ev.Measured {
				spent += ev.SpendNanoUSD
			}
			if ev.Failed || !ev.Measured {
				r.ErrorStage = ev.Kind
			}
		}
		if e != nil && r.ErrorStage == "" {
			r.ErrorStage = "runtime"
		}
		bundle.Sessions = append(bundle.Sessions, r)
		if e != nil || r.ErrorStage != "" {
			fillSkipped()
			t.Errorf("stopped at %s (%s); inspect provider usage before continuing", sc.ID, r.ErrorStage)
			return
		}
	}
	if len(bundle.Sessions) != n {
		t.Error("case count exceeds partition")
	}
	t.Logf("cases=%d measured nano USD=%d", len(bundle.Sessions), spent)
}

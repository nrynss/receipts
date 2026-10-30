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
	for _, p := range []string{"../judge/openai.go", "../claims/openai.go", "../claims/sensitive.go", "../retrieval/openai.go", "../retrieval/retrieval.go", "../intelligence/service.go", "../intelligence/policy.go", "../providerusage/usage.go", "../commands/receipt.go", "../store/detect.go", "../store/embeddings.go", "../store/live_transcript.go", "../store/store.go", "session.go", "session_bundle.go", "trace.go", "freeze.go", "checkpoint.go", "checkpoint_adapters.go", "collect_sessions_live_test.go", "../../cmd/receipts-eval-sessions/main.go"} {
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
		if json.Unmarshal(freezeRaw, &freeze) != nil || ValidateDevelopmentFreeze(freeze, hash, reportHash, sources["../judge/openai.go"], sources["../intelligence/policy.go"], devCount, reportRaw) != nil {
			t.Fatal("invalid policy freeze")
		}
		bundle.FreezeSHA256 = freezeHash
	}

	bundle.Requested = n
	bundle.BudgetNanoUSD = limit
	resume := os.Getenv("RECEIPTS_EVAL_RESUME") == "1"
	checkpoint, e := OpenCheckpoint(path, bundle, resume)
	if e != nil {
		t.Fatalf("checkpoint unavailable: %v", e)
	}
	defer func() {
		if err := checkpoint.Close(); err != nil {
			t.Error("checkpoint lock release failed")
		}
	}()
	var selected []SessionCase
	for _, sc := range cases {
		if sc.Split == split {
			selected = append(selected, sc)
		}
	}
	if n > len(selected) {
		t.Fatal("case count exceeds partition")
	}
	selected = selected[:n]
	pending, e := checkpoint.Remaining(selected)
	if e != nil {
		t.Fatal("checkpoint case sequence mismatch")
	}
	seconds := 540
	if v := os.Getenv("RECEIPTS_EVAL_MAX_RUNTIME_SECONDS"); v != "" {
		seconds, e = strconv.Atoi(v)
		if e != nil || seconds < 1 || seconds > 1140 {
			t.Fatal("runtime must be 1..1140 seconds")
		}
	}
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	if td, ok := t.Deadline(); ok && td.Add(-45*time.Second).Before(deadline) {
		deadline = td.Add(-45 * time.Second)
	}
	if !deadline.After(time.Now()) {
		t.Fatal("go test timeout too short")
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	ledgerRoot := path + ".ledgers"
	if e = os.MkdirAll(ledgerRoot, 0700); e != nil {
		t.Fatal("ledger directory unavailable")
	}
	info, e := os.Lstat(ledgerRoot)
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		t.Fatal("unsafe ledger directory")
	}
	httpClient := &http.Client{Transport: DigestTransport{}}
	client := claims.Client{APIKey: key, HTTP: httpClient}
	embed := retrieval.Client{APIKey: key, HTTP: httpClient, Space: retrieval.Space{Model: retrieval.DefaultModel, Version: "evaluation-v1", Dimensions: retrieval.DefaultDimensions}}
	j := judge.Client{APIKey: key, HTTP: httpClient}
	for _, sc := range pending {
		if ctx.Err() != nil {
			if e = checkpoint.Stop("deadline"); e != nil {
				t.Error("checkpoint save failed")
			}
			t.Error("collection deadline reached")
			return
		}
		spent, e := checkpoint.Spent()
		if e != nil {
			t.Fatal("invalid checkpoint spend")
		}
		inProgress := len(checkpoint.Bundle.Sessions) > 0 && checkpoint.Bundle.Sessions[len(checkpoint.Bundle.Sessions)-1].ID == sc.ID
		if !inProgress && limit-spent < 500000000 {
			if e = checkpoint.Stop("budget_exhausted"); e != nil {
				t.Error("checkpoint save failed")
			}
			t.Error("budget insufficient for next session")
			return
		}
		idx, e := checkpoint.StartCase(sc.ID)
		if e != nil {
			t.Fatalf("case unavailable: %v", e)
		}
		ledgerDir, e := os.MkdirTemp(ledgerRoot, sc.ID+"-")
		if e != nil {
			t.Fatal("ledger directory unavailable")
		}
		checkpoint.Bundle.Sessions[idx].LedgerDirectories = append(checkpoint.Bundle.Sessions[idx].LedgerDirectories, ledgerDir)
		if e = checkpoint.Save(); e != nil {
			t.Fatal("ledger checkpoint failed")
		}
		base := SessionAdapters{Extractor: client, Sensitive: client, Embedder: embed, Judge: j, Dimensions: retrieval.DefaultDimensions, Budget: cost.Price(500000000)}
		adapters, check := ResumeAdapters(base, checkpoint)
		begin := time.Now()
		result, runErr := RunSession(ctx, ledgerDir, sc, adapters)
		elapsed := time.Since(begin).Milliseconds()
		checkpoint.Bundle.Sessions[idx].LatencyMS += elapsed
		if runErr == nil {
			runErr = check()
		}
		if runErr != nil {
			stage := "runtime"
			if ctx.Err() != nil {
				stage = "deadline"
			} else if errors.Is(runErr, ErrCheckpointBudget) {
				stage = "budget_exhausted"
			}
			if e = checkpoint.Stop(stage); e != nil {
				t.Error("checkpoint save failed")
			}
			t.Errorf("collection stopped at %s (%s); reconcile before retry", sc.ID, stage)
			return
		}
		if e = checkpoint.FinishCase(result, 0); e != nil {
			t.Fatal("case checkpoint failed")
		}
	}
	if e = checkpoint.Complete(); e != nil {
		t.Fatal("completion checkpoint failed")
	}
	spent, e := checkpoint.Spent()
	if e != nil {
		t.Fatal("invalid checkpoint spend")
	}
	t.Logf("cases=%d measured nano USD=%d", len(checkpoint.Bundle.Sessions), spent)
}

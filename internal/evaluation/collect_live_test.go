//go:build live

package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"receipts/internal/claims"
	"receipts/internal/judge"
	"receipts/internal/providerusage"
	"receipts/internal/receipt"
	"receipts/internal/retrieval"
)

// TestCollectLive collects one partition prefix without sending labels to the
// provider. It is never part of the offline gate. Each call is reserved before
// execution and the run stops on ambiguous usage or any provider error.
func TestCollectLive(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY required")
	}
	split := os.Getenv("RECEIPTS_EVAL_SPLIT")
	if split != "dev" {
		t.Fatal("legacy collector supports dev only; set RECEIPTS_EVAL_SPLIT")
	}
	n, e := strconv.Atoi(os.Getenv("RECEIPTS_EVAL_CASES"))
	if e != nil || n < 1 || n > 44 {
		t.Fatal("set case count 1..44")
	}
	budget, e := strconv.ParseInt(os.Getenv("RECEIPTS_EVAL_BUDGET_NANO_USD"), 10, 64)
	if e != nil || budget < 77000000 || budget > 4000000000 {
		t.Fatal("budget must be 77000000..4000000000 nano USD")
	}
	path := os.Getenv("RECEIPTS_EVAL_OUTPUT")
	if path == "" {
		t.Fatal("output required")
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	raw, e := os.ReadFile("../../testdata/eval/pairs.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	pairs, hash, e := ReadPairs(raw)
	if e != nil {
		t.Fatal(e)
	}
	sourceHash := func(path string) string {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		h := sha256.Sum256(b)
		return hex.EncodeToString(h[:])
	}
	b := Bundle{Manifest: Manifest{DatasetSHA256: hash, Split: split, Provenance: "live-provider-sanitized", JudgeModel: judge.Model, EmbeddingModel: retrieval.DefaultModel, Policy: "review-only-v1", JudgeSourceSHA256: sourceHash("../judge/openai.go"), RetrievalSourceSHA256: sourceHash("../retrieval/retrieval.go"), PolicySourceSHA256: sourceHash("../intelligence/policy.go")}}
	spent := int64(0)
	call := func(reserve int64, fn func(context.Context) error) error {
		if budget-spent < reserve {
			return errBudget
		}
		var u cost.Usage
		ctx := providerusage.WithCollector(context.Background(), func(v cost.Usage) { u = v })
		e := fn(ctx)
		if !u.Measured {
			spent += reserve
			return errUsage
		}
		spent += int64(u.Price)
		if int64(u.Price) > reserve {
			return errBudget
		}
		return e
	}
	defer func() {
		if e := json.NewEncoder(f).Encode(b); e != nil {
			t.Errorf("write sanitized bundle: %v", e)
		}
		t.Logf("recorded cases=%d accounted nano USD=%d", len(b.Records), spent)
	}()
	space := retrieval.Space{Model: retrieval.DefaultModel, Version: "evaluation-v1", Dimensions: retrieval.DefaultDimensions}
	emb := retrieval.Client{APIKey: key, Space: space}
	sensitive := claims.Client{APIKey: key, Model: claims.LegacyModel}
	j := judge.Client{APIKey: key}
	for _, p := range pairs {
		if p.Split != split {
			continue
		}
		if len(b.Records) >= n {
			break
		}
		r := Recording{ID: p.ID}
		start := time.Now()
		before := spent
		stage := "later_sensitivity"
		e = call(25000000, func(c context.Context) error {
			var err error
			r.LaterSensitive, err = sensitive.Sensitive(c, p.Later)
			return err
		})
		if e == nil && !r.LaterSensitive {
			stage = "claim_sensitivity"
			e = call(25000000, func(c context.Context) error {
				var err error
				r.ClaimSensitive, err = sensitive.Sensitive(c, p.Claim)
				return err
			})
		}
		if e == nil && !r.LaterSensitive && !r.ClaimSensitive {
			stage = "claim_embedding"
			e = call(1000000, func(c context.Context) error { var err error; r.ClaimVector, err = emb.Embed(c, p.Claim); return err })
			if e == nil {
				stage = "later_embedding"
				e = call(1000000, func(c context.Context) error { var err error; r.LaterVector, err = emb.Embed(c, p.Later); return err })
			}
			if e == nil {
				stage = "judge"
				claim := receipt.Receipt{ID: p.ID, Claim: p.Claim}
				ev := receipt.Evidence{ReceiptID: p.ID, SourceID: p.ID + ":claim", Text: p.Claim, Attribution: "broadcaster"}
				later := receipt.Segment{ID: p.ID + ":later", Text: p.Later, Attribution: "broadcaster"}
				e = call(25000000, func(c context.Context) error {
					v, err := j.Judge(c, claim, ev, later)
					r.Verdict = v.Verdict
					r.ClaimSourceID = v.ClaimSourceID
					r.LaterSourceID = v.LaterSourceID
					return err
				})
			}
		}
		r.LatencyMS = time.Since(start).Milliseconds()
		r.SpendNanoUSD = spent - before
		r.UsageMeasured = e == nil
		if e != nil {
			r.ErrorStage = stage
		}
		b.Records = append(b.Records, r)
		if e != nil {
			t.Errorf("collection stopped at %s (%s); inspect provider usage before retry", p.ID, stage)
			return
		}
	}
	if len(b.Records) != n {
		t.Error("requested more cases than partition contains")
	}
}

var errBudget = errors.New("budget exhausted")
var errUsage = errors.New("usage unavailable")

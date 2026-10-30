//go:build live

package judge

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/nrynss/keel/cost"
	"receipts/internal/providerusage"
	"receipts/internal/receipt"
)

// TestLiveOneCase performs exactly one paid request; no automatic retries.
func TestLiveOneCase(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY is required")
	}
	budget, err := strconv.ParseFloat(os.Getenv("RECEIPTS_T53_LIVE_BUDGET_USD"), 64)
	if err != nil || budget < 0.025 || budget > 0.05 {
		t.Fatal("set RECEIPTS_T53_LIVE_BUDGET_USD between 0.025 and 0.05")
	}
	claim := receipt.Receipt{ID: "receipt", Claim: "I will never take the shortcut."}
	original := receipt.Evidence{ReceiptID: "receipt", SourceID: "original", Text: claim.Claim, Attribution: "broadcaster"}
	later := receipt.Segment{ID: "later", Text: "I will take the shortcut.", Attribution: "broadcaster"}
	var usage cost.Usage
	ctx := providerusage.WithCollector(context.Background(), func(u cost.Usage) { usage = u })
	result, err := (Client{APIKey: key}).Judge(ctx, claim, original, later)
	if err != nil {
		t.Fatalf("judge request failed: %v", err)
	}
	if !usage.Measured {
		t.Fatal("provider usage missing; check provider spend before any further run")
	}
	if float64(usage.Price)/1e9 > budget {
		t.Fatal("measured cost exceeded budget; stop")
	}
	t.Logf("verdict=%s, measured USD %.9f", result.Verdict, float64(usage.Price)/1e9)
}

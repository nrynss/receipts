package intelligence

import (
	"testing"

	"receipts/internal/detect"
	"receipts/internal/receipt"
)

func TestJudgmentOutcomeKeepsAutomaticStampsDisabled(t *testing.T) {
	for _, tc := range []struct {
		verdict detect.Verdict
		want    receipt.DetectionOutcome
	}{
		{detect.Contradicts, receipt.NeedsReview},
		{detect.Unsure, receipt.NeedsReview},
		{detect.DoesNot, receipt.Dismissed},
	} {
		got, err := JudgmentOutcome(tc.verdict)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v; want %q", tc.verdict, got, err, tc.want)
		}
	}
	if _, err := JudgmentOutcome("invalid"); err == nil {
		t.Fatal("invalid verdict accepted")
	}
}

package intelligence

import (
	"errors"

	"receipts/internal/detect"
	"receipts/internal/receipt"
)

// JudgmentOutcome is the current production decision policy. A contradiction
// needs private review until independent evaluation supports automatic stamps.
// It deliberately accepts no confidence threshold: the judge provides none.
func JudgmentOutcome(verdict detect.Verdict) (receipt.DetectionOutcome, error) {
	switch verdict {
	case detect.Contradicts, detect.Unsure:
		return receipt.NeedsReview, nil
	case detect.DoesNot:
		return receipt.Dismissed, nil
	default:
		return "", errors.New("intelligence: invalid judgment")
	}
}

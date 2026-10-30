package evaluation

import (
	"encoding/json"
	"errors"
)

// ValidateDevelopmentFreeze rejects diagnostics, failures, and incomplete
// development reports before any held-out collection can start.
func ValidateDevelopmentFreeze(f Freeze, datasetHash, reportHash, judgeHash, policyHash string, expected int, report []byte) error {
	var dev struct {
		DatasetSHA256, Split, Policy string
		Total, Errors                int
		Complete, DiagnosticOnly     bool
	}
	if json.Unmarshal(report, &dev) != nil || expected <= 0 || f.DatasetSHA256 != datasetHash || f.Policy != "review-only-v1" || f.DevReportSHA256 != reportHash || f.JudgeSourceSHA256 != judgeHash || f.PolicySourceSHA256 != policyHash || dev.DatasetSHA256 != datasetHash || dev.Split != "dev" || dev.Policy != "review-only-v1" || dev.Total != expected || dev.Errors != 0 || !dev.Complete || dev.DiagnosticOnly {
		return errors.New("invalid policy freeze")
	}
	return nil
}

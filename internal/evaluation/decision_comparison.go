package evaluation

import (
	"context"
	"errors"
	"time"

	"github.com/nrynss/keel/cost"
	"receipts/internal/claims"
	"receipts/internal/judge"
	"receipts/internal/providerusage"
	"receipts/internal/receipt"
)

type ComparisonJudge interface {
	Judge(context.Context, receipt.Receipt, receipt.Evidence, receipt.Segment) (judge.Result, error)
}

type ComparisonSensitivity interface {
	Classify(context.Context, string) (claims.SensitivityDecision, error)
}

// CompareDecisionCase records one same-input legacy/Decisions comparison. The
// checkpoint reserves and persists each provider attempt before it runs, then
// persists usage before the sanitized result. Labels are not included in either
// request. An unresolved or unmeasured attempt blocks checkpoint resume.
func CompareDecisionCase(ctx context.Context, c *Checkpoint, p Pair, legacySensitivity, decisionsSensitivity ComparisonSensitivity, legacy, decisions ComparisonJudge, legacyReserve, decisionsReserve int64) error {
	if c == nil || legacySensitivity == nil || decisionsSensitivity == nil || legacy == nil || decisions == nil || p.ID == "" || p.Split != c.Bundle.Split || p.Claim == "" || p.Later == "" || legacyReserve <= 0 || decisionsReserve <= 0 {
		return errors.New("invalid comparison case")
	}
	started := time.Now()
	if _, err := c.StartCase(p.ID); err != nil {
		return err
	}
	claim := receipt.Receipt{ID: p.ID, Claim: p.Claim}
	evidence := receipt.Evidence{ReceiptID: p.ID, SourceID: p.ID + ":claim", Text: p.Claim, Attribution: "broadcaster"}
	later := receipt.Segment{ID: p.ID + ":later", Text: p.Later, Attribution: "broadcaster"}
	legacyClaim, err := checkpointComparisonSensitivity(ctx, c, "legacy-claim", "gpt-4o-mini-2024-07-18", legacyReserve, legacySensitivity, p.Claim)
	if err != nil {
		_ = c.Stop("legacy_sensitivity_error")
		return err
	}
	decisionsClaim, err := checkpointComparisonSensitivity(ctx, c, "decisions-claim", judge.DecisionsModel, decisionsReserve, decisionsSensitivity, p.Claim)
	if err != nil {
		_ = c.Stop("decisions_sensitivity_error")
		return err
	}
	legacyLater, err := checkpointComparisonSensitivity(ctx, c, "legacy-later", "gpt-4o-mini-2024-07-18", legacyReserve, legacySensitivity, p.Later)
	if err != nil {
		_ = c.Stop("legacy_sensitivity_error")
		return err
	}
	decisionsLater, err := checkpointComparisonSensitivity(ctx, c, "decisions-later", judge.DecisionsModel, decisionsReserve, decisionsSensitivity, p.Later)
	if err != nil {
		_ = c.Stop("decisions_sensitivity_error")
		return err
	}
	outcomes := map[string]string{
		"legacy_claim_sensitive":    boolString(isSensitive(legacyClaim)),
		"decisions_claim_sensitive": boolString(isSensitive(decisionsClaim)),
		"legacy_later_sensitive":    boolString(isSensitive(legacyLater)),
		"decisions_later_sensitive": boolString(isSensitive(decisionsLater)),
	}
	// Preserve the local sensitivity boundary: if either model flags either
	// text, neither judge receives that text in the paired experiment.
	if isSensitive(legacyClaim) || isSensitive(decisionsClaim) || isSensitive(legacyLater) || isSensitive(decisionsLater) {
		return c.FinishCase(SessionResult{Outcomes: outcomes}, time.Since(started).Milliseconds())
	}
	legacyResult, err := checkpointComparisonCall(ctx, c, "legacy", "gpt-4o-mini-2024-07-18", legacyReserve, legacy, claim, evidence, later)
	if err != nil {
		_ = c.Stop("legacy_judge_error")
		return err
	}
	decisionsResult, err := checkpointComparisonCall(ctx, c, "decisions", judge.DecisionsModel, decisionsReserve, decisions, claim, evidence, later)
	if err != nil {
		_ = c.Stop("decisions_judge_error")
		return err
	}
	outcomes["legacy_verdict"] = string(legacyResult.Verdict)
	outcomes["decisions_verdict"] = string(decisionsResult.Verdict)
	return c.FinishCase(SessionResult{Outcomes: outcomes}, time.Since(started).Milliseconds())
}

func checkpointComparisonSensitivity(ctx context.Context, c *Checkpoint, kind, model string, reserve int64, adapter ComparisonSensitivity, text string) (claims.SensitivityDecision, error) {
	key := inputHash([]string{model, text})
	event := SessionEvent{Kind: "sensitive-comparison-" + kind, Model: model, InputSHA256: key}
	i, err := c.Begin(event, reserve)
	if err != nil {
		return claims.SensitivityDecision{}, err
	}
	event.State = "reserved"
	event.ReservationNanoUSD = reserve
	var checkpointErr error
	callCtx := context.WithValue(ctx, requestDigestKey{}, func(v string) { event.RequestSHA256 = v })
	callCtx = providerusage.WithObserver(callCtx, func(u cost.Usage) {
		event.Measured = u.Measured
		event.SpendNanoUSD = int64(u.Price)
		if u.Measured {
			event.State = "measured"
		}
		if x := c.Update(i, event); x != nil {
			checkpointErr = x
		}
	})
	out, callErr := adapter.Classify(callCtx, text)
	if checkpointErr != nil {
		return claims.SensitivityDecision{}, errors.New("comparison usage checkpoint failed")
	}
	event.Failed = callErr != nil
	if x := c.Update(i, event); x != nil {
		return claims.SensitivityDecision{}, errors.New("comparison attempt checkpoint failed")
	}
	if callErr != nil {
		return claims.SensitivityDecision{}, callErr
	}
	if !event.Measured || event.SpendNanoUSD > reserve {
		return claims.SensitivityDecision{}, errors.New("comparison sensitivity result or measured usage unavailable")
	}
	if out.Choice != "sensitive" && out.Choice != "not_sensitive" && out.Choice != "unsure" {
		return claims.SensitivityDecision{}, errors.New("comparison sensitivity choice unavailable")
	}
	event.Sensitive = isSensitive(out)
	event.Model = model
	event.Choice = out.Choice
	event.Confidence = &out.Confidence
	event.Probabilities = out.Probabilities
	event.State = "complete"
	if x := c.Update(i, event); x != nil {
		return claims.SensitivityDecision{}, errors.New("comparison response checkpoint failed")
	}
	return out, nil
}

func checkpointComparisonCall(ctx context.Context, c *Checkpoint, kind, model string, reserve int64, adapter ComparisonJudge, claim receipt.Receipt, evidence receipt.Evidence, later receipt.Segment) (judge.Result, error) {
	key := inputHash([]string{model, claim.Claim, evidence.SourceID, evidence.Text, later.ID, later.Text})
	event := SessionEvent{Kind: "judge-comparison-" + kind, Model: model, InputSHA256: key}
	i, err := c.Begin(event, reserve)
	if err != nil {
		return judge.Result{}, err
	}
	event.State = "reserved"
	event.ReservationNanoUSD = reserve
	var checkpointErr error
	callCtx := context.WithValue(ctx, requestDigestKey{}, func(v string) { event.RequestSHA256 = v })
	callCtx = providerusage.WithObserver(callCtx, func(u cost.Usage) {
		event.Measured = u.Measured
		event.SpendNanoUSD = int64(u.Price)
		if u.Measured {
			event.State = "measured"
		}
		if x := c.Update(i, event); x != nil {
			checkpointErr = x
		}
	})
	out, callErr := adapter.Judge(callCtx, claim, evidence, later)
	if checkpointErr != nil {
		return judge.Result{}, errors.New("comparison usage checkpoint failed")
	}
	event.Failed = callErr != nil
	if x := c.Update(i, event); x != nil {
		return judge.Result{}, errors.New("comparison attempt checkpoint failed")
	}
	if callErr != nil {
		return judge.Result{}, callErr
	}
	if !event.Measured || event.SpendNanoUSD > reserve || out.ClaimSourceID != evidence.SourceID || out.LaterSourceID != later.ID || (out.Verdict != "contradicts" && out.Verdict != "does_not_contradict" && out.Verdict != "unsure") {
		return judge.Result{}, errors.New("comparison answer or measured usage unavailable")
	}
	event.Judgment = out
	event.Model = model
	event.State = "complete"
	if x := c.Update(i, event); x != nil {
		return judge.Result{}, errors.New("comparison response checkpoint failed")
	}
	return out, nil
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func isSensitive(d claims.SensitivityDecision) bool {
	return d.Choice != "not_sensitive"
}

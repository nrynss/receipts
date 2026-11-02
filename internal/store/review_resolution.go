package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"receipts/internal/events"
	"receipts/internal/receipt"
)

// ReviewRefusal identifies the failed approval stage without exposing evidence.
// Unwrap preserves the existing eligibility and conflict error contracts.
type ReviewRefusal struct {
	Reason string
	Cause  error
}

func (e *ReviewRefusal) Error() string { return "review refused: " + e.Reason }
func (e *ReviewRefusal) Unwrap() error { return e.Cause }

type ReviewResolution struct {
	Replayed bool
	Receipt  *events.PublicReceipt
}

// ReviewCandidate does not expose evidence. The final transaction checks this
// version again, so intervening changes cannot silently approve a new state.
func (s *Store) ReviewCandidate(ctx context.Context, accountID, channelID, reviewID string) (receipt.Receipt, error) {
	var id string
	err := s.db.Reader().QueryRowContext(ctx, `SELECT r.id FROM reviews v JOIN detections d ON d.id=v.detection_id JOIN receipts r ON r.id=d.receipt_id JOIN accounts a ON a.channel_id=r.channel_id WHERE v.id=? AND a.id=? AND a.status='connected' AND r.channel_id=? AND d.channel_id=r.channel_id`, reviewID, accountID, channelID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt.Receipt{}, ErrNotFound
	}
	if err != nil {
		return receipt.Receipt{}, err
	}
	return s.GetReceipt(ctx, id)
}

// ResolveReviewAction accepts a review ID (never a detection ID). Runtime
// admission and the public publisher lock are held by the application service.
func (s *Store) ResolveReviewAction(ctx context.Context, a DetectionAuthority, candidate receipt.Receipt, reviewID, key string, approve bool, paused bool, cooldown time.Duration) (result ReviewResolution, err error) {
	stage := "input"
	defer func() {
		if err != nil {
			err = &ReviewRefusal{Reason: stage, Cause: err}
		}
	}()
	if a.AccountID == "" || a.ChannelID == "" || a.LiveSessionID == "" || a.Generation == 0 || candidate.ID == "" || reviewID == "" || key == "" || len(key) > 200 || cooldown < 0 {
		return result, ErrInvalid
	}
	stage = "transaction"
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	stage = "live_authority"
	var active int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts a JOIN live_sessions l ON l.channel_id=a.channel_id WHERE a.id=? AND a.status='connected' AND a.channel_id=? AND l.id=? AND l.state='live' AND l.ended_at IS NULL`, a.AccountID, a.ChannelID, a.LiveSessionID).Scan(&active)
	if err != nil {
		return result, err
	}
	if active != 1 {
		return result, ErrNotEligible
	}
	decision := "reject"
	if approve {
		decision = "approve"
	}
	stage = "action_replay"
	var priorReview, priorDecision string
	err = tx.QueryRowContext(ctx, `SELECT review_id,decision FROM review_actions WHERE channel_id=? AND action_key=?`, a.ChannelID, key).Scan(&priorReview, &priorDecision)
	if err == nil {
		if priorReview != reviewID || priorDecision != decision {
			return result, ErrConflict
		}
		result.Replayed = true
		return result, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	stage = "candidate_version"
	var state, detectionID, sourceSession, sourceID, text, attribution, claim, clippedBy, clipURL, clipStatus, verdict, validity string
	var start, end, created int64
	var sensitive bool
	var expires sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT v.state,d.id,d.session_id,d.source_id,d.start_ms,d.end_ms,d.evidence_text,d.attribution,r.claim,r.clipped_by,r.clip_url,r.clip_status,r.verdict,r.created_at,r.validity,r.sensitive,r.expires_at FROM reviews v JOIN detections d ON d.id=v.detection_id JOIN receipts r ON r.id=d.receipt_id WHERE v.id=? AND d.channel_id=? AND r.channel_id=? AND r.id=? AND r.version=? AND r.claim=?`, reviewID, a.ChannelID, a.ChannelID, candidate.ID, candidate.Version, candidate.Claim).Scan(&state, &detectionID, &sourceSession, &sourceID, &start, &end, &text, &attribution, &claim, &clippedBy, &clipURL, &clipStatus, &verdict, &created, &validity, &sensitive, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotEligible
	}
	if err != nil {
		return result, err
	}
	stage = "review_state"
	if state != "open" {
		return result, ErrConflict
	}
	now := s.now().UTC()
	if approve {
		stage = "paused"
		if paused {
			return result, ErrNotEligible
		}
		stage = "source_session_or_receipt_eligibility"
		if sourceSession != a.LiveSessionID || validity != "valid" || sensitive || attribution != "broadcaster" || (expires.Valid && expires.Int64 <= millis(now)) {
			return result, ErrNotEligible
		}
		stage = "exact_live_source"
		var exact int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM transcript_segments t JOIN live_sessions l ON l.id=t.session_id WHERE t.id=? AND t.session_id=? AND t.start_ms=? AND t.end_ms=? AND t.text=? AND t.attribution='broadcaster' AND l.channel_id=? AND l.state='live' AND l.ended_at IS NULL AND l.started_at+t.end_ms<=?`, sourceID, a.LiveSessionID, start, end, text, a.ChannelID, millis(now)).Scan(&exact)
		if err != nil {
			return result, err
		}
		if exact != 1 {
			return result, ErrNotEligible
		}
		stage = "stamp_cooldown"
		var recent int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM stamp_history WHERE channel_id=? AND stamped_at>?`, a.ChannelID, millis(now.Add(-cooldown))).Scan(&recent); err != nil {
			return result, err
		}
		if recent != 0 {
			return result, ErrNotEligible
		}
		stage = "stamp_write"
		if _, err = tx.ExecContext(ctx, `INSERT INTO stamp_history(detection_id,channel_id,stamped_at) VALUES(?,?,?)`, detectionID, a.ChannelID, millis(now)); err != nil {
			return result, err
		}
		result.Receipt = &events.PublicReceipt{ID: candidate.ID, Claim: claim, ClippedBy: clippedBy, ClipURL: clipURL, ClipStatus: clipStatus, Verdict: verdict, CreatedAt: time.UnixMilli(created).UTC()}
	}
	stage = "decision_write"
	nextState, outcome := "rejected", "dismissed"
	if approve {
		nextState, outcome = "approved", "stamp"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE reviews SET state=?,decided_at=? WHERE id=? AND state='open'`, nextState, millis(now), reviewID); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE detections SET outcome=? WHERE id=? AND outcome='review'`, outcome, detectionID); err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO review_actions(channel_id,action_key,review_id,decision) VALUES(?,?,?,?)`, a.ChannelID, key, reviewID, decision); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	stage = "commit"
	return result, tx.Commit()
}

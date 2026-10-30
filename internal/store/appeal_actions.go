package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"receipts/internal/receipt"
)

// ReceiptActionResult identifies the one durable result of an owner action.
// Repeated calls with the same key and payload return the original result.
type ReceiptActionResult struct {
	ID, Kind, ReceiptID string
	Replayed            bool
}

// ApplyReceiptAction validates live ownership, current stamp and eligibility in
// the same write transaction as the action. The runtime serializes this call
// against session rollover and publisher snapshots.
func (s *Store) ApplyReceiptAction(ctx context.Context, a DetectionAuthority, receiptID, actionKey, kind, note string, duration time.Duration) (ReceiptActionResult, error) {
	var out ReceiptActionResult
	if a.AccountID == "" || a.ChannelID == "" || a.LiveSessionID == "" || a.Generation == 0 || receiptID == "" || actionKey == "" || len(actionKey) > 200 || len(note) > 4000 || (kind != "admit" && kind != "context_changed" && kind != "appeal") || (kind == "appeal" && duration <= 0) || (kind == "context_changed" && strings.TrimSpace(note) == "") {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts a JOIN live_sessions l ON l.channel_id=a.channel_id WHERE a.id=? AND a.channel_id=? AND a.status='connected' AND l.id=? AND l.state='live' AND l.ended_at IS NULL`, a.AccountID, a.ChannelID, a.LiveSessionID).Scan(&count)
	if err != nil {
		return out, err
	}
	if count != 1 {
		return out, ErrNotEligible
	}
	var priorReceipt, priorKind, priorNote, priorID string
	err = tx.QueryRowContext(ctx, `SELECT receipt_id,kind,note,result_id FROM receipt_actions WHERE channel_id=? AND action_key=?`, a.ChannelID, actionKey).Scan(&priorReceipt, &priorKind, &priorNote, &priorID)
	if err == nil {
		if priorReceipt != receiptID || priorKind != kind || priorNote != note {
			return out, ErrConflict
		}
		if err = tx.Commit(); err != nil {
			return out, err
		}
		return ReceiptActionResult{ID: priorID, Kind: kind, ReceiptID: receiptID, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	now := s.now().UTC()
	var detectionID string
	err = tx.QueryRowContext(ctx, `SELECT d.id FROM detections d JOIN receipts r ON r.id=d.receipt_id WHERE d.id=(SELECT latest.id FROM detections latest LEFT JOIN stamp_history h ON h.detection_id=latest.id WHERE latest.channel_id=? AND latest.session_id=? AND latest.outcome='stamp' ORDER BY coalesce(h.stamped_at,latest.created_at) DESC,latest.id DESC LIMIT 1) AND r.channel_id=d.channel_id AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?)`, a.ChannelID, a.LiveSessionID, millis(now)).Scan(&detectionID)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotEligible
	}
	if err != nil {
		return out, err
	}
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM detections WHERE id=? AND receipt_id=? AND NOT EXISTS (SELECT 1 FROM receipt_actions WHERE detection_id=?)`, detectionID, receiptID, detectionID).Scan(&count)
	if err != nil {
		return out, err
	}
	if count != 1 {
		return out, ErrConflict
	}

	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM appeals p JOIN receipts r ON r.id=p.receipt_id WHERE r.channel_id=? AND p.state='open'`, a.ChannelID).Scan(&count)
	if err != nil {
		return out, err
	}
	if count != 0 {
		return out, ErrConflict
	}
	id, err := s.newID()
	if err != nil {
		return out, err
	}
	if kind == "appeal" {
		_, err = tx.ExecContext(ctx, `INSERT INTO appeals(id,receipt_id,state,opened_at,closes_at) VALUES(?,?,'open',?,?)`, id, receiptID, millis(now), millis(now.Add(duration)))
	} else {
		outcome := receipt.Admitted
		if kind == "context_changed" {
			outcome = receipt.ContextChanged
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO verdicts(id,receipt_id,decision_key,outcome,note,created_at) VALUES(?,?,?,?,?,?)`, id, receiptID, "owner-action:"+actionKey, outcome, note, millis(now))
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE receipts SET verdict=?,version=version+1 WHERE id=?`, outcome, receiptID)
		}
	}
	if err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO receipt_actions(channel_id,action_key,receipt_id,detection_id,kind,note,result_id) VALUES(?,?,?,?,?,?,?)`, a.ChannelID, actionKey, receiptID, detectionID, kind, note, id)
	if err != nil {
		return out, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return ReceiptActionResult{ID: id, Kind: kind, ReceiptID: receiptID}, nil
}

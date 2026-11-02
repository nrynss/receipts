package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"receipts/internal/receipt"
)

// OwnedChannelSession validates the durable account/channel association rather
// than trusting either identifier supplied to a scoped application reader.
func (s *Store) OwnedChannelSession(ctx context.Context, accountID, channelID, sessionID string) (receipt.Session, error) {
	var value receipt.Session
	var started int64
	var ended sql.NullInt64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT l.id,l.channel_id,l.started_at,l.ended_at,l.state FROM live_sessions l JOIN accounts a ON a.channel_id=l.channel_id WHERE a.id=? AND a.status='connected' AND l.channel_id=? AND l.id=?`, accountID, channelID, sessionID).Scan(&value.ID, &value.ChannelID, &started, &ended, &value.State)
	if errors.Is(err, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	if err != nil {
		return value, err
	}
	value.StartedAt = time.UnixMilli(started).UTC()
	value.EndedAt = date(ended)
	return value, nil
}

// VerifyLiveSession is the activation gate for runtime supervisors. Login is
// not a live-session signal; only a persisted, owner-bound live session passes.
func (s *Store) VerifyLiveSession(ctx context.Context, accountID, channelID, sessionID string) error {
	value, err := s.OwnedChannelSession(ctx, accountID, channelID, sessionID)
	if err != nil {
		return err
	}
	if value.State != "live" || value.EndedAt != nil {
		return ErrNotEligible
	}
	return nil
}

type PendingChannelReview struct {
	ID           string
	Claim        string
	EvidenceText string
	CreatedAt    time.Time
}

// PendingReviews omits voided, sensitive and expired claims even in the
// owner's quiet queue. Account ownership is part of the query itself.
func (s *Store) PendingReviews(ctx context.Context, accountID, channelID string, limit int) ([]PendingChannelReview, error) {
	return s.PendingReviewsForSession(ctx, accountID, channelID, "", limit)
}

// PendingReviewsForSession returns only reviews the live authority can resolve.
// Historical reviews remain durable but never occupy the actionable queue.
func (s *Store) PendingReviewsForSession(ctx context.Context, accountID, channelID, sessionID string, limit int) ([]PendingChannelReview, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT v.id,r.claim,d.evidence_text,v.created_at FROM reviews v JOIN detections d ON d.id=v.detection_id JOIN receipts r ON r.id=d.receipt_id JOIN accounts a ON a.channel_id=r.channel_id JOIN live_sessions l ON l.id=d.session_id AND l.channel_id=d.channel_id WHERE a.id=? AND a.status='connected' AND r.channel_id=? AND d.channel_id=r.channel_id AND l.state='live' AND l.ended_at IS NULL AND (?='' OR l.id=?) AND v.state='open' AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?) ORDER BY v.created_at,v.id LIMIT ?`, accountID, channelID, sessionID, sessionID, millis(s.now()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]PendingChannelReview, 0)
	for rows.Next() {
		var v PendingChannelReview
		var at int64
		if err = rows.Scan(&v.ID, &v.Claim, &v.EvidenceText, &at); err != nil {
			return nil, err
		}
		v.CreatedAt = time.UnixMilli(at).UTC()
		values = append(values, v)
	}
	return values, rows.Err()
}

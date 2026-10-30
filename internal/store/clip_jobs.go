package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"receipts/internal/receipt"
)

// ClaimDueClipJobs atomically leases a bounded batch for one active session
// generation. Expired leases make interrupted jobs recoverable after restart.
func (s *Store) ClaimDueClipJobs(ctx context.Context, a DetectionAuthority, limit int, lease time.Duration) ([]receipt.ClipJob, error) {
	if a.AccountID == "" || a.ChannelID == "" || a.LiveSessionID == "" || a.Generation == 0 {
		return nil, ErrInvalid
	}
	if limit <= 0 || limit > 1 {
		limit = 1
	}
	if lease <= 0 {
		lease = 10 * time.Second
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	var connected int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts a JOIN live_sessions l ON l.channel_id=a.channel_id WHERE a.id=? AND a.channel_id=? AND a.status='connected' AND l.id=? AND l.state='live' AND l.ended_at IS NULL`, a.AccountID, a.ChannelID, a.LiveSessionID).Scan(&connected); err != nil {
		return nil, err
	}
	if connected != 1 {
		return nil, ErrNotEligible
	}
	rows, err := tx.QueryContext(ctx, `SELECT j.id,j.channel_id,j.receipt_id,c.command_id,j.requested_at,j.status,j.clip_id,j.clip_url,j.last_error,j.attempts,j.clip_requested_at,j.next_attempt_at,j.polls,j.create_started,(SELECT l.started_at+MAX(e.end_ms) FROM evidence e JOIN live_sessions l ON l.id=r.session_id WHERE e.receipt_id=r.id) FROM clip_jobs j JOIN receipt_captures c ON c.receipt_id=j.receipt_id JOIN receipts r ON r.id=j.receipt_id WHERE j.channel_id=? AND r.session_id=? AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?) AND j.status='pending' AND j.next_attempt_at<=? AND j.lease_until<=? ORDER BY j.requested_at,j.id LIMIT ?`, a.ChannelID, a.LiveSessionID, millis(now), millis(now), millis(now), limit)
	if err != nil {
		return nil, err
	}
	out := make([]receipt.ClipJob, 0, limit)
	for rows.Next() {
		var job receipt.ClipJob
		var requested, clipRequested, next int64
		var createStarted int
		var evidenceEnds sql.NullInt64
		if err = rows.Scan(&job.ID, &job.ChannelID, &job.ReceiptID, &job.CommandID, &requested, &job.Status, &job.ClipID, &job.ClipURL, &job.LastError, &job.Attempts, &clipRequested, &next, &job.Polls, &createStarted, &evidenceEnds); err != nil {
			rows.Close()
			return nil, err
		}
		job.RequestedAt = time.UnixMilli(requested).UTC()
		if clipRequested != 0 {
			job.ClipRequestedAt = time.UnixMilli(clipRequested).UTC()
		}
		if next != 0 {
			job.NextAttemptAt = time.UnixMilli(next).UTC()
		}
		job.LeaseGeneration = a.Generation
		job.CreateStarted = createStarted != 0
		if evidenceEnds.Valid {
			job.EvidenceEndsAt = time.UnixMilli(evidenceEnds.Int64).UTC()
		}
		if !job.ClipRequestedAt.IsZero() && !job.EvidenceEndsAt.IsZero() {
			job.CaptureLag = job.ClipRequestedAt.Sub(job.EvidenceEndsAt)
		}
		out = append(out, job)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		token, e := s.newID()
		if e != nil {
			return nil, e
		}
		out[i].LeaseToken = token
		res, e := tx.ExecContext(ctx, `UPDATE clip_jobs SET lease_until=?,claim_generation=?,lease_token=? WHERE id=? AND status='pending' AND lease_until<=?`, millis(now.Add(lease)), a.Generation, token, out[i].ID, millis(now))
		if e != nil {
			return nil, e
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return nil, ErrConflict
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveClipJobAttempt persists a retry deadline and safe diagnostic only while
// the job remains pending. A clip ID is immutable once recorded.
func (s *Store) SaveClipJobAttempt(ctx context.Context, a DetectionAuthority, job receipt.ClipJob, clipID, detail string, next time.Time) error {
	if job.ID == "" || job.ChannelID == "" || next.IsZero() || a.ChannelID != job.ChannelID || a.Generation != job.LeaseGeneration {
		return ErrInvalid
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var eligible int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM clip_jobs j JOIN receipts r ON r.id=j.receipt_id JOIN accounts a ON a.channel_id=j.channel_id JOIN live_sessions l ON l.id=r.session_id WHERE j.id=? AND j.channel_id=? AND j.status='pending' AND j.claim_generation=? AND j.lease_token=? AND j.lease_until>? AND a.id=? AND a.status='connected' AND r.session_id=? AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?) AND l.state='live' AND l.ended_at IS NULL`, job.ID, job.ChannelID, a.Generation, job.LeaseToken, millis(s.now()), a.AccountID, a.LiveSessionID, millis(s.now())).Scan(&eligible)
	if err != nil {
		return err
	}
	if eligible != 1 {
		return ErrNotEligible
	}
	var oldID string
	err = tx.QueryRowContext(ctx, `SELECT clip_id FROM clip_jobs WHERE id=? AND channel_id=? AND status='pending'`, job.ID, job.ChannelID).Scan(&oldID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if oldID != "" && clipID != "" && oldID != clipID {
		return ErrConflict
	}
	if clipID == "" {
		clipID = oldID
	}
	res, err := tx.ExecContext(ctx, `UPDATE clip_jobs SET clip_id=?,clip_requested_at=CASE WHEN ?<>'' AND clip_requested_at=0 THEN ? ELSE clip_requested_at END,create_started=CASE WHEN ?<>'' THEN 0 ELSE create_started END,attempts=attempts+CASE WHEN ?<>'' THEN 1 ELSE 0 END,polls=polls+CASE WHEN clip_id<>'' AND ?='' THEN 1 ELSE 0 END,last_error=?,next_attempt_at=?,lease_until=0,lease_token='' WHERE id=? AND channel_id=? AND status='pending' AND claim_generation=? AND lease_token=? AND lease_until>?`, clipID, clipID, millis(s.now()), clipID, detail, clipID, detail, millis(next), job.ID, job.ChannelID, a.Generation, job.LeaseToken, millis(s.now()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return tx.Commit()
}

// CompleteClipJobBound applies a delayed outcome only while the originating
// owner, live session, receipt and runtime-generation lease still match.
func (s *Store) CompleteClipJobBound(ctx context.Context, a DetectionAuthority, job receipt.ClipJob, status, clipID, clipURL, detail string) error {
	if a.AccountID == "" || a.ChannelID == "" || a.LiveSessionID == "" || a.Generation == 0 || job.ID == "" || job.LeaseToken == "" || job.ChannelID != a.ChannelID || (status != "ready" && status != "unavailable" && status != "failed") || (status == "ready" && (clipID == "" || clipURL == "")) {
		return ErrInvalid
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var receiptID, oldStatus, oldID, oldURL, oldDetail string
	err = tx.QueryRowContext(ctx, `SELECT j.receipt_id,j.status,j.clip_id,j.clip_url,j.last_error FROM clip_jobs j JOIN receipts r ON r.id=j.receipt_id JOIN accounts a ON a.channel_id=j.channel_id JOIN live_sessions l ON l.id=r.session_id WHERE j.id=? AND j.channel_id=? AND j.status='pending' AND j.claim_generation=? AND j.lease_token=? AND j.lease_until>? AND a.id=? AND a.status='connected' AND r.session_id=? AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?) AND l.state='live' AND l.ended_at IS NULL`, job.ID, a.ChannelID, a.Generation, job.LeaseToken, millis(s.now()), a.AccountID, a.LiveSessionID, millis(s.now())).Scan(&receiptID, &oldStatus, &oldID, &oldURL, &oldDetail)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotEligible
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE clip_jobs SET status=?,clip_id=?,clip_url=?,last_error=?,capture_lag_ms=?,lease_until=0,lease_token='' WHERE id=? AND status='pending' AND claim_generation=? AND lease_token=? AND lease_until>?`, status, clipID, clipURL, detail, job.CaptureLag.Milliseconds(), job.ID, a.Generation, job.LeaseToken, millis(s.now()))
	if err != nil {
		return err
	}
	clipStatus := receipt.ClipUnavailable
	if status == "ready" {
		clipStatus = receipt.ClipReady
	}
	_, err = tx.ExecContext(ctx, `UPDATE receipts SET clip_status=?,clip_id=?,clip_url=?,version=version+1 WHERE id=? AND channel_id=? AND session_id=? AND validity='valid' AND sensitive=0`, clipStatus, clipID, clipURL, receiptID, a.ChannelID, a.LiveSessionID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// MarkClipCreateStarted durably fences the non-idempotent POST. If a process
// disappears after this marker but before saving Twitch's ID, recovery fails
// the job as uncertain instead of issuing a second POST.
func (s *Store) MarkClipCreateStarted(ctx context.Context, a DetectionAuthority, job receipt.ClipJob) error {
	if a.AccountID == "" || a.ChannelID != job.ChannelID || a.LiveSessionID == "" || job.ID == "" || job.LeaseToken == "" || job.LeaseGeneration != a.Generation || job.CreateStarted || job.ClipID != "" {
		return ErrInvalid
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE clip_jobs SET create_started=1,clip_requested_at=? WHERE id=? AND channel_id=? AND status='pending' AND clip_id='' AND create_started=0 AND claim_generation=? AND lease_token=? AND lease_until>? AND EXISTS(SELECT 1 FROM receipts r JOIN accounts a ON a.channel_id=r.channel_id JOIN live_sessions l ON l.id=r.session_id WHERE r.id=clip_jobs.receipt_id AND a.id=? AND a.status='connected' AND r.session_id=? AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?) AND l.state='live' AND l.ended_at IS NULL)`, millis(s.now()), job.ID, a.ChannelID, a.Generation, job.LeaseToken, millis(s.now()), a.AccountID, a.LiveSessionID, millis(s.now()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotEligible
	}
	return tx.Commit()
}

// ResetClipCreateStarted is permitted only after an explicit 429 response,
// which confirms Twitch rejected the POST without creating a clip.
func (s *Store) ResetClipCreateStarted(ctx context.Context, a DetectionAuthority, job receipt.ClipJob) error {
	if a.AccountID == "" || a.ChannelID != job.ChannelID || job.ID == "" || job.LeaseToken == "" {
		return ErrInvalid
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE clip_jobs SET create_started=0 WHERE id=? AND channel_id=? AND status='pending' AND clip_id='' AND create_started=1 AND claim_generation=? AND lease_token=? AND lease_until>? AND EXISTS(SELECT 1 FROM receipts r JOIN accounts a ON a.channel_id=r.channel_id JOIN live_sessions l ON l.id=r.session_id WHERE r.id=clip_jobs.receipt_id AND a.id=? AND a.status='connected' AND r.session_id=? AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?) AND l.state='live' AND l.ended_at IS NULL)`, job.ID, a.ChannelID, a.Generation, job.LeaseToken, millis(s.now()), a.AccountID, a.LiveSessionID, millis(s.now()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotEligible
	}
	return tx.Commit()
}

package auth

import (
	"context"
	"errors"
)

// EraseDisconnectedAccount serializes deletion with provider reauthorization.
// cleanup must drain work and erase external per-channel state first.
func (s *Service) EraseDisconnectedAccount(ctx context.Context, accountID string, cleanup func(context.Context, string) error) error {
	subject, err := s.accountSubject(ctx, accountID)
	if err != nil {
		return err
	}
	lock := s.accountLock(subject)
	lock.Lock()
	defer lock.Unlock()
	var channel, status string
	if err = s.db.Reader().QueryRowContext(ctx, `SELECT channel_id,status FROM accounts WHERE id=?`, accountID).Scan(&channel, &status); err != nil || status != "reconnect_required" {
		return errors.New("auth: account must be disconnected before deletion")
	}
	if cleanup == nil {
		return errors.New("auth: deletion unavailable")
	}
	if err = cleanup(ctx, channel); err != nil {
		return err
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`DELETE FROM stamp_history WHERE channel_id=?`,
		`DELETE FROM detections WHERE channel_id=?`,
		`DELETE FROM clip_jobs WHERE channel_id=?`,
		`DELETE FROM receipt_captures WHERE channel_id=?`,
		`DELETE FROM receipt_commands WHERE channel_id=?`,
		`DELETE FROM verdicts WHERE receipt_id IN (SELECT id FROM receipts WHERE channel_id=?)`,
		`DELETE FROM receipts WHERE channel_id=?`,
		`DELETE FROM live_sessions WHERE channel_id=?`,
		`DELETE FROM accounts WHERE channel_id=?`,
		`DELETE FROM channels WHERE id=?`,
	}
	for _, q := range statements {
		if _, err = tx.ExecContext(ctx, q, channel); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.pairMu.Lock()
	for code, p := range s.pairs {
		if p.accountID == accountID {
			delete(s.pairs, code)
		}
	}
	s.pairMu.Unlock()
	return nil
}

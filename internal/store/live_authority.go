package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"receipts/internal/receipt"
)

type liveAuthoritySnapshot struct {
	id, broadcastID, state                                    string
	startedAt, statusCheckedAt, microphoneAllowed, generation int64
	endedAt                                                   sql.NullInt64
}

// LiveActivation is a persisted broadcast session with a fresh runtime
// generation. The rollback snapshot is private and only accepted by Store.
type LiveActivation struct {
	Session    receipt.Session
	Generation uint64
	previous   *liveAuthoritySnapshot
}

// StartOwnedLiveSession persists only a freshly verified broadcaster stream,
// reusing its ledger session on same-broadcast runtime restart.
func (s *Store) StartOwnedLiveSession(ctx context.Context, accountID, channelID, broadcastID string, checkedAt time.Time, microphoneAllowed bool) (LiveActivation, error) {
	if accountID == "" || channelID == "" || broadcastID == "" || checkedAt.IsZero() {
		return LiveActivation{}, ErrInvalid
	}
	key, err := s.idSource()
	if err != nil {
		return LiveActivation{}, err
	}
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return LiveActivation{}, err
	}
	defer tx.Rollback()
	var owned int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts a JOIN channels c ON c.id=a.channel_id WHERE a.id=? AND a.channel_id=? AND a.status='connected'`, accountID, channelID).Scan(&owned); err != nil {
		return LiveActivation{}, err
	}
	if owned != 1 {
		return LiveActivation{}, ErrNotFound
	}
	var previous liveAuthoritySnapshot
	err = tx.QueryRowContext(ctx, `SELECT id,broadcast_id,started_at,ended_at,state,status_checked_at,microphone_allowed,runtime_generation FROM live_sessions WHERE channel_id=? AND state='live' AND ended_at IS NULL`, channelID).Scan(&previous.id, &previous.broadcastID, &previous.startedAt, &previous.endedAt, &previous.state, &previous.statusCheckedAt, &previous.microphoneAllowed, &previous.generation)
	hasPrevious := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return LiveActivation{}, err
	}
	checked := millis(checkedAt)
	mic := int64(0)
	if microphoneAllowed {
		mic = 1
	}
	var activation LiveActivation
	if hasPrevious && previous.broadcastID == broadcastID {
		generation := previous.generation + 1
		if _, err = tx.ExecContext(ctx, `UPDATE live_sessions SET status_checked_at=?,microphone_allowed=?,runtime_generation=? WHERE id=? AND channel_id=? AND state='live'`, checked, mic, generation, previous.id, channelID); err != nil {
			return LiveActivation{}, err
		}
		activation = LiveActivation{Session: receipt.Session{ID: previous.id, ChannelID: channelID, StartedAt: time.UnixMilli(previous.startedAt).UTC(), State: "live"}, Generation: uint64(generation), previous: &previous}
	} else {
		now := millis(s.now())
		if hasPrevious {
			if _, err = tx.ExecContext(ctx, `UPDATE live_sessions SET state='ended',ended_at=?,status_checked_at=0,microphone_allowed=0 WHERE id=? AND state='live'`, now, previous.id); err != nil {
				return LiveActivation{}, err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO live_sessions(id,channel_id,started_at,state,broadcast_id,status_checked_at,microphone_allowed,runtime_generation) VALUES(?,?,?,'live',?,?,?,1)`, key, channelID, now, broadcastID, checked, mic)
		if err != nil {
			return LiveActivation{}, err
		}
		activation = LiveActivation{Session: receipt.Session{ID: key, ChannelID: channelID, StartedAt: time.UnixMilli(now).UTC(), State: "live"}, Generation: 1}
		if hasPrevious {
			activation.previous = &previous
		}
	}
	if err = tx.Commit(); err != nil {
		return LiveActivation{}, err
	}
	return activation, nil
}

// RollbackLiveActivation restores only the session version this activation
// wrote. A delayed failure cannot undo a newer broadcast or runtime generation.
func (s *Store) RollbackLiveActivation(ctx context.Context, activation LiveActivation) error {
	if activation.Session.ID == "" || activation.Session.ChannelID == "" || activation.Generation == 0 {
		return ErrInvalid
	}
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var broadcastID string
	var generation int64
	var state string
	err = tx.QueryRowContext(ctx, `SELECT broadcast_id,runtime_generation,state FROM live_sessions WHERE id=? AND channel_id=?`, activation.Session.ID, activation.Session.ChannelID).Scan(&broadcastID, &generation, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if state != "live" || uint64(generation) != activation.Generation {
		return ErrConflict
	}
	now := millis(s.now())
	if activation.previous == nil {
		_, err = tx.ExecContext(ctx, `UPDATE live_sessions SET state='ended',ended_at=?,status_checked_at=0,microphone_allowed=0 WHERE id=? AND runtime_generation=?`, now, activation.Session.ID, generation)
	} else if activation.previous.id == activation.Session.ID {
		p := activation.previous
		_, err = tx.ExecContext(ctx, `UPDATE live_sessions SET broadcast_id=?,started_at=?,ended_at=?,state=?,status_checked_at=?,microphone_allowed=?,runtime_generation=? WHERE id=? AND runtime_generation=?`, p.broadcastID, p.startedAt, nullInt(p.endedAt), p.state, p.statusCheckedAt, p.microphoneAllowed, p.generation, activation.Session.ID, generation)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE live_sessions SET state='ended',ended_at=?,status_checked_at=0,microphone_allowed=0 WHERE id=? AND runtime_generation=?`, now, activation.Session.ID, generation)
		if err == nil {
			p := activation.previous
			_, err = tx.ExecContext(ctx, `UPDATE live_sessions SET ended_at=?,state=?,status_checked_at=?,microphone_allowed=?,runtime_generation=? WHERE id=?`, nullInt(p.endedAt), p.state, p.statusCheckedAt, p.microphoneAllowed, p.generation, p.id)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ResetLiveAuthority revokes freshness and mic consent at process startup
// while preserving ledger session IDs, receipts, votes, and appeal deadlines.
func (s *Store) ResetLiveAuthority(ctx context.Context) error {
	_, err := s.db.Writer().ExecContext(ctx, `UPDATE live_sessions SET status_checked_at=0,microphone_allowed=0,runtime_generation=runtime_generation+1 WHERE state='live'`)
	return err
}

func (s *Store) VerifyLiveGeneration(ctx context.Context, accountID, channelID, sessionID string, generation uint64) error {
	if generation == 0 {
		return ErrNotEligible
	}
	var current uint64
	var broadcastID string
	err := s.db.Reader().QueryRowContext(ctx, `SELECT l.runtime_generation,l.broadcast_id FROM live_sessions l JOIN accounts a ON a.channel_id=l.channel_id WHERE a.id=? AND a.channel_id=? AND a.status='connected' AND l.id=? AND l.state='live' AND l.ended_at IS NULL`, accountID, channelID, sessionID).Scan(&current, &broadcastID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && current != generation && !(current == 0 && broadcastID == "")) {
		return ErrNotEligible
	}
	return err
}

func (s *Store) CurrentLiveAuthority(ctx context.Context, accountID, channelID string) (string, string, time.Time, bool, uint64, error) {
	var sessionID, broadcastID string
	var checked, mic, generation int64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT l.id,l.broadcast_id,l.status_checked_at,l.microphone_allowed,l.runtime_generation FROM live_sessions l JOIN accounts a ON a.channel_id=l.channel_id WHERE a.id=? AND a.channel_id=? AND a.status='connected' AND l.state='live' AND l.ended_at IS NULL`, accountID, channelID).Scan(&sessionID, &broadcastID, &checked, &mic, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", time.Time{}, false, 0, ErrNotFound
	}
	if err != nil {
		return "", "", time.Time{}, false, 0, err
	}
	return sessionID, broadcastID, time.UnixMilli(checked).UTC(), mic == 1, uint64(generation), nil
}

func (s *Store) TouchLiveAuthority(ctx context.Context, accountID, channelID, sessionID, broadcastID string, generation uint64, checkedAt time.Time) error {
	if accountID == "" || channelID == "" || sessionID == "" || broadcastID == "" || generation == 0 || checkedAt.IsZero() {
		return ErrInvalid
	}
	result, err := s.db.Writer().ExecContext(ctx, `UPDATE live_sessions SET status_checked_at=? WHERE id=? AND channel_id=? AND broadcast_id=? AND state='live' AND runtime_generation=? AND EXISTS(SELECT 1 FROM accounts a WHERE a.id=? AND a.channel_id=live_sessions.channel_id AND a.status='connected')`, millis(checkedAt), sessionID, channelID, broadcastID, generation, accountID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ClearLivePermission(ctx context.Context, channelID, sessionID string, generation uint64) error {
	if channelID == "" || sessionID == "" || generation == 0 {
		return ErrInvalid
	}
	result, err := s.db.Writer().ExecContext(ctx, `UPDATE live_sessions SET status_checked_at=0,microphone_allowed=0 WHERE id=? AND channel_id=? AND state='live' AND runtime_generation=?`, sessionID, channelID, generation)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) EndSessionGeneration(ctx context.Context, channelID, sessionID string, generation uint64) error {
	if generation == 0 {
		return ErrInvalid
	}
	res, err := s.db.Writer().ExecContext(ctx, `UPDATE live_sessions SET state='ended',ended_at=?,status_checked_at=0,microphone_allowed=0 WHERE id=? AND channel_id=? AND state='live' AND runtime_generation=?`, millis(s.now()), sessionID, channelID, generation)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return nil
}

func nullInt(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

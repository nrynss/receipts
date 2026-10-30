package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"receipts/internal/events"
	"receipts/internal/receipt"
)

func validActionAuthority(ctx context.Context, tx *sql.Tx, a DetectionAuthority) error {
	if a.AccountID == "" || a.ChannelID == "" || a.LiveSessionID == "" || a.Generation == 0 {
		return ErrInvalid
	}
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts a JOIN live_sessions l ON l.channel_id=a.channel_id WHERE a.id=? AND a.channel_id=? AND a.status='connected' AND l.id=? AND l.state='live' AND l.ended_at IS NULL`, a.AccountID, a.ChannelID, a.LiveSessionID).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotEligible
	}
	return nil
}

// ActiveAppeal includes only public counts, never voter IDs.
type ActiveAppeal struct {
	ID     string
	Public events.PublicAppeal
}

func (s *Store) ActiveAppeal(ctx context.Context, channelID string) (*ActiveAppeal, error) {
	var a ActiveAppeal
	var closes int64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT a.id,a.receipt_id,a.closes_at, count(v.id) FILTER (WHERE v.choice='uphold'), count(v.id) FILTER (WHERE v.choice='overturn') FROM appeals a JOIN receipts r ON r.id=a.receipt_id LEFT JOIN votes v ON v.appeal_id=a.id WHERE r.channel_id=? AND a.state='open' AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?) GROUP BY a.id`, channelID, millis(s.now())).Scan(&a.ID, &a.Public.ReceiptID, &closes, &a.Public.UpholdVotes, &a.Public.OverturnVotes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Public.ClosesAt = time.UnixMilli(closes).UTC()
	return &a, nil
}

// VoteActive uses trusted user ID, and the server clock rather than client time.
func (s *Store) VoteActive(ctx context.Context, a DetectionAuthority, voter string, choice receipt.VoteChoice) (bool, error) {
	if voter == "" || (choice != receipt.VoteUphold && choice != receipt.VoteOverturn) {
		return false, ErrInvalid
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err = validActionAuthority(ctx, tx, a); err != nil {
		return false, err
	}
	now := millis(s.now())
	var appealID string
	err = tx.QueryRowContext(ctx, `SELECT p.id FROM appeals p JOIN receipts r ON r.id=p.receipt_id WHERE r.channel_id=? AND p.state='open' AND p.opened_at<=? AND p.closes_at>? AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?)`, a.ChannelID, now, now, now).Scan(&appealID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotEligible
	}
	if err != nil {
		return false, err
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT choice FROM votes WHERE appeal_id=? AND voter_id=?`, appealID, voter).Scan(&existing)
	if err == nil {
		if existing != string(choice) {
			return false, ErrConflict
		}
		return false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	id, err := s.newID()
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO votes(id,appeal_id,voter_id,choice,created_at) VALUES(?,?,?,?,?)`, id, appealID, voter, choice, now)
	if err != nil {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// CloseDueAppeal closes one overdue or invalidated appeal once. Invalidated or
// expired claims are cancelled without publishing a verdict.
func (s *Store) CloseDueAppeal(ctx context.Context, a DetectionAuthority) (bool, error) {
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err = validActionAuthority(ctx, tx, a); err != nil {
		return false, err
	}
	now := millis(s.now())
	var id, rid, valid string
	var closes int64
	var sensitive int
	var expires sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT p.id,p.receipt_id,p.closes_at,r.validity,r.sensitive,r.expires_at FROM appeals p JOIN receipts r ON r.id=p.receipt_id WHERE r.channel_id=? AND p.state='open'`, a.ChannelID).Scan(&id, &rid, &closes, &valid, &sensitive, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	cancelled := valid != "valid" || sensitive != 0 || (expires.Valid && expires.Int64 <= now)
	if !cancelled && now < closes {
		return false, nil
	}
	outcome := "cancelled"
	if !cancelled {
		var up, down int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE choice='uphold'),count(*) FILTER (WHERE choice='overturn') FROM votes WHERE appeal_id=?`, id).Scan(&up, &down); err != nil {
			return false, err
		}
		outcome = string(receipt.Inconclusive)
		if up > down {
			outcome = string(receipt.Upheld)
		} else if down > up {
			outcome = string(receipt.Overturned)
		}
		verdictID, e := s.newID()
		if e != nil {
			return false, e
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO verdicts(id,receipt_id,appeal_id,decision_key,outcome,created_at) VALUES(?,?,?,?,?,?)`, verdictID, rid, id, "appeal-close:"+id, outcome, now)
		if err != nil {
			return false, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE receipts SET verdict=?,version=version+1 WHERE id=?`, outcome, rid)
		if err != nil {
			return false, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE appeals SET state='closed',decision=?,decided_at=? WHERE id=? AND state='open'`, outcome, now, id)
	if err != nil {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) VoidOwned(ctx context.Context, a DetectionAuthority, rid string) (bool, error) {
	if rid == "" {
		return false, ErrInvalid
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err = validActionAuthority(ctx, tx, a); err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE receipts SET validity='void',version=version+1 WHERE id=? AND channel_id=? AND validity='valid'`, rid, a.ChannelID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM receipts WHERE id=? AND channel_id=?`, rid, a.ChannelID).Scan(&exists)
		if err != nil {
			return false, err
		}
		if exists == 0 {
			return false, ErrNotFound
		}
		return false, tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `UPDATE reviews SET state='rejected',decided_at=? WHERE detection_id IN (SELECT id FROM detections WHERE receipt_id=?) AND state='open'`, millis(s.now()), rid)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE appeals SET state='closed',decision='void',decided_at=? WHERE receipt_id=? AND state='open'`, millis(s.now()), rid)
	if err != nil {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// NextAppealDeadline includes invalid claims so recovery cannot strand them.
func (s *Store) NextAppealDeadline(ctx context.Context, channelID string) (*time.Time, error) {
	var closes int64
	var expires sql.NullInt64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT p.closes_at,r.expires_at FROM appeals p JOIN receipts r ON r.id=p.receipt_id WHERE r.channel_id=? AND p.state='open'`, channelID).Scan(&closes, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if expires.Valid && expires.Int64 < closes {
		closes = expires.Int64
	}
	v := time.UnixMilli(closes).UTC()
	return &v, nil
}

// RecoverDueAppeals is a privileged startup sweep. It closes overdue or
// invalidated work without asserting that any channel is live or emitting a
// stamp. The caller runs it before accepting requests or starting runtimes.
func (s *Store) RecoverDueAppeals(ctx context.Context) (int, error) {
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := millis(s.now())
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.receipt_id,r.validity,r.sensitive,r.expires_at FROM appeals p JOIN receipts r ON r.id=p.receipt_id WHERE p.state='open' AND (p.closes_at<=? OR r.validity<>'valid' OR r.sensitive<>0 OR (r.expires_at IS NOT NULL AND r.expires_at<=?)) ORDER BY p.id`, now, now)
	if err != nil {
		return 0, err
	}
	type due struct {
		id, rid, valid string
		sensitive      int
		expires        sql.NullInt64
	}
	var values []due
	for rows.Next() {
		var d due
		if err = rows.Scan(&d.id, &d.rid, &d.valid, &d.sensitive, &d.expires); err != nil {
			rows.Close()
			return 0, err
		}
		values = append(values, d)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	for _, d := range values {
		outcome := "cancelled"
		valid := d.valid == "valid" && d.sensitive == 0 && (!d.expires.Valid || d.expires.Int64 > now)
		if valid {
			var up, down int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE choice='uphold'),count(*) FILTER (WHERE choice='overturn') FROM votes WHERE appeal_id=?`, d.id).Scan(&up, &down); err != nil {
				return 0, err
			}
			outcome = string(receipt.Inconclusive)
			if up > down {
				outcome = string(receipt.Upheld)
			} else if down > up {
				outcome = string(receipt.Overturned)
			}
			id, e := s.newID()
			if e != nil {
				return 0, e
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO verdicts(id,receipt_id,appeal_id,decision_key,outcome,created_at) VALUES(?,?,?,?,?,?)`, id, d.rid, d.id, "appeal-close:"+d.id, outcome, now); err != nil {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE receipts SET verdict=?,version=version+1 WHERE id=?`, outcome, d.rid); err != nil {
				return 0, err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE appeals SET state='closed',decision=?,decided_at=? WHERE id=? AND state='open'`, outcome, now, d.id); err != nil {
			return 0, err
		}
	}
	if err = ctx.Err(); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(values), nil
}

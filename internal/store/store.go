// Package store owns the durable receipt ledger.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/sqlite"
	"receipts/internal/receipt"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const migrationNamespace = "receipts_ledger"

var (
	ErrNotFound    = errors.New("store: record not found")
	ErrConflict    = errors.New("store: invalid state transition")
	ErrInvalid     = errors.New("store: invalid input")
	ErrNotEligible = errors.New("store: record is not eligible")
)

// Retention bounds the rolling transcript window. ExpireAfter=0 disables age
// expiry; MaxSegments must be positive and caps rows per session.
type Retention struct {
	ExpireAfter time.Duration
	MaxSegments int
}
type Config struct {
	Path      string
	Retention Retention
	Now       func() time.Time
	IDSource  func() (string, error)
}

type Store struct {
	db        *sqlite.DB
	retention Retention
	now       func() time.Time
	idSource  func() (string, error)
	ownsDB    bool
}

func Open(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("%w: database path is required", ErrInvalid)
	}
	if cfg.Retention.MaxSegments <= 0 {
		cfg.Retention.MaxSegments = 600
	}
	if cfg.Retention.ExpireAfter < 0 {
		return nil, fmt.Errorf("%w: transcript expiry cannot be negative", ErrInvalid)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	idSource := cfg.IDSource
	if idSource == nil {
		idSource = id.New
	}
	db, err := sqlite.Open(ctx, sqlite.Config{Path: cfg.Path, Now: now})
	if err != nil {
		return nil, err
	}
	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err == nil {
		err = sqlite.Migrate(ctx, db, migrationNamespace, migrations)
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, retention: cfg.Retention, now: now, idSource: idSource, ownsDB: true}, nil
}

// New migrates a caller-owned Keel SQLite handle. Closing this Store does not
// close db; this lets the ledger share the application's one database file.
func New(ctx context.Context, db *sqlite.DB, retention Retention, now func() time.Time) (*Store, error) {
	return NewWithIDSource(ctx, db, retention, now, nil)
}

// NewWithIDSource migrates a caller-owned DB and allows deterministic callers
// such as fixture replay to supply stable IDs. A nil source keeps Keel IDs.
func NewWithIDSource(ctx context.Context, db *sqlite.DB, retention Retention, now func() time.Time, idSource func() (string, error)) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: sqlite handle is required", ErrInvalid)
	}
	if retention.MaxSegments <= 0 {
		retention.MaxSegments = 600
	}
	if retention.ExpireAfter < 0 {
		return nil, ErrInvalid
	}
	if now == nil {
		now = time.Now
	}
	if idSource == nil {
		idSource = id.New
	}
	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	if err = sqlite.Migrate(ctx, db, migrationNamespace, migrations); err != nil {
		return nil, err
	}
	return &Store{db: db, retention: retention, now: now, idSource: idSource}, nil
}

func (s *Store) Close() error {
	if !s.ownsDB {
		return nil
	}
	return s.db.Close()
}

func (s *Store) newID() (string, error) { return s.idSource() }
func millis(t time.Time) int64          { return t.UTC().UnixMilli() }
func date(ms sql.NullInt64) *time.Time {
	if !ms.Valid {
		return nil
	}
	t := time.UnixMilli(ms.Int64).UTC()
	return &t
}
func strNull(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

func (s *Store) CreateChannel(ctx context.Context, platformID, login string) (receipt.Channel, error) {
	if platformID == "" || login == "" {
		return receipt.Channel{}, ErrInvalid
	}
	now := s.now().UTC()
	candidate, err := s.newID()
	if err != nil {
		return receipt.Channel{}, err
	}
	_, err = s.db.Writer().ExecContext(ctx, `INSERT INTO channels(id,platform_id,login,created_at) VALUES(?,?,?,?) ON CONFLICT(platform_id) DO NOTHING`, candidate, platformID, login, millis(now))
	if err != nil {
		return receipt.Channel{}, err
	}
	return scanChannel(s.db.Reader().QueryRowContext(ctx, `SELECT id,platform_id,login,created_at FROM channels WHERE platform_id=?`, platformID))
}

func scanChannel(row *sql.Row) (receipt.Channel, error) {
	var c receipt.Channel
	var created int64
	err := row.Scan(&c.ID, &c.PlatformID, &c.Login, &created)
	c.CreatedAt = time.UnixMilli(created).UTC()
	return c, err
}

func (s *Store) StartSession(ctx context.Context, channelID string) (receipt.Session, error) {
	if channelID == "" {
		return receipt.Session{}, ErrInvalid
	}
	now := s.now().UTC()
	key, err := s.newID()
	if err != nil {
		return receipt.Session{}, err
	}
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return receipt.Session{}, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM channels WHERE id=?`, channelID).Scan(&exists); err != nil {
		return receipt.Session{}, err
	}
	if exists == 0 {
		return receipt.Session{}, ErrNotFound
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_sessions SET state='ended',ended_at=?,microphone_allowed=0 WHERE channel_id=? AND state='live'`, millis(now), channelID)
	if err != nil {
		return receipt.Session{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO live_sessions(id,channel_id,started_at,state) VALUES(?,?,?,'live')`, key, channelID, millis(now))
	if err != nil {
		return receipt.Session{}, err
	}
	if err = tx.Commit(); err != nil {
		return receipt.Session{}, err
	}
	return receipt.Session{ID: key, ChannelID: channelID, StartedAt: now, State: "live"}, nil
}

func (s *Store) EndSession(ctx context.Context, channelID, sessionID string) error {
	res, err := s.db.Writer().ExecContext(ctx, `UPDATE live_sessions SET state='ended',ended_at=?,microphone_allowed=0 WHERE id=? AND channel_id=? AND state='live'`, millis(s.now()), sessionID, channelID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) ReceiptSession(ctx context.Context, channelID, sessionID string) (receipt.Session, error) {
	var sess receipt.Session
	var started int64
	var ended sql.NullInt64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT id,channel_id,started_at,ended_at,state FROM live_sessions WHERE id=? AND channel_id=?`, sessionID, channelID).Scan(&sess.ID, &sess.ChannelID, &started, &ended, &sess.State)
	if errors.Is(err, sql.ErrNoRows) {
		return sess, ErrNotFound
	}
	if err != nil {
		return sess, err
	}
	sess.StartedAt = time.UnixMilli(started).UTC()
	sess.EndedAt = date(ended)
	return sess, nil
}

// AddSegment inserts a session-relative transcript segment and trims the session
// to both its configured age window and row bound within the same transaction.
func (s *Store) AddSegment(ctx context.Context, seg receipt.Segment) error {
	if seg.SessionID == "" || seg.Start < 0 || seg.End < seg.Start || seg.Text == "" {
		return ErrInvalid
	}
	if seg.ID == "" {
		var err error
		seg.ID, err = s.newID()
		if err != nil {
			return err
		}
	}
	// The storage clock owns retention timestamps; callers cannot make stale
	// transcript rows immortal by supplying an old or future capture timestamp.
	seg.CreatedAt = s.now().UTC()
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var live int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM live_sessions WHERE id=? AND state='live'`, seg.SessionID).Scan(&live); err != nil {
		return err
	}
	if live == 0 {
		return ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO transcript_segments(id,session_id,start_ms,end_ms,text,speaker_id,attribution,created_at) VALUES(?,?,?,?,?,?,?,?)`, seg.ID, seg.SessionID, seg.Start.Milliseconds(), seg.End.Milliseconds(), seg.Text, seg.SpeakerID, defaultText(seg.Attribution, "uncertain"), millis(seg.CreatedAt))
	if err != nil {
		return err
	}
	if s.retention.ExpireAfter > 0 {
		_, err = tx.ExecContext(ctx, `DELETE FROM transcript_segments WHERE created_at < ?`, millis(seg.CreatedAt.Add(-s.retention.ExpireAfter)))
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM transcript_segments WHERE id NOT IN (SELECT id FROM transcript_segments ORDER BY created_at DESC, start_ms DESC LIMIT ?)`, s.retention.MaxSegments)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func defaultText(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
func validateSource(ctx context.Context, tx *sql.Tx, sessionID string, e receipt.EvidenceInput) error {
	if e.SourceID == "" || e.Text == "" || e.Start < 0 || e.End < e.Start {
		return ErrInvalid
	}
	var start, end int64
	var text, attribution string
	err := tx.QueryRowContext(ctx, `SELECT start_ms,end_ms,text,attribution FROM transcript_segments WHERE id=? AND session_id=?`, e.SourceID, sessionID).Scan(&start, &end, &text, &attribution)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if e.Start.Milliseconds() < start || e.End.Milliseconds() > end || e.Text != text || e.Attribution != attribution {
		return ErrInvalid
	}
	return nil
}

func (s *Store) Transcript(ctx context.Context, sessionID string) ([]receipt.Segment, error) {
	query := `SELECT id,session_id,start_ms,end_ms,text,speaker_id,attribution,created_at,timing_basis FROM transcript_segments WHERE session_id=?`
	args := []any{sessionID}
	if s.retention.ExpireAfter > 0 {
		query += ` AND created_at>=?`
		args = append(args, millis(s.now().Add(-s.retention.ExpireAfter)))
	}
	query += ` ORDER BY start_ms`
	rows, err := s.db.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []receipt.Segment{}
	for rows.Next() {
		var v receipt.Segment
		var a, b, created int64
		if err = rows.Scan(&v.ID, &v.SessionID, &a, &b, &v.Text, &v.SpeakerID, &v.Attribution, &created, &v.TimingBasis); err != nil {
			return nil, err
		}
		v.Start = time.Duration(a) * time.Millisecond
		v.End = time.Duration(b) * time.Millisecond
		v.CreatedAt = time.UnixMilli(created).UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

// BeginReceiptCommand durably deduplicates before extraction. Throttles are
// scoped to the channel and authenticated Twitch user, with a channel-wide cap.
// A failed process leaves a processing marker, preventing duplicate work.
func (s *Store) BeginReceiptCommand(ctx context.Context, channelID, sessionID, commandID, userID string, receivedAt time.Time) (string, error) {
	if channelID == "" || sessionID == "" || commandID == "" || userID == "" || receivedAt.IsZero() {
		return "", ErrInvalid
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var live int
	var startedMillis int64
	if err = tx.QueryRowContext(ctx, `SELECT count(*),coalesce(max(started_at),0) FROM live_sessions WHERE id=? AND channel_id=? AND state='live'`, sessionID, channelID).Scan(&live, &startedMillis); err != nil {
		return "", err
	}
	if live == 0 {
		return "", ErrConflict
	}
	serverNow := s.now().UTC()
	if receivedAt.Before(time.UnixMilli(startedMillis).UTC()) || receivedAt.After(serverNow) || serverNow.Sub(receivedAt) > 30*time.Second {
		return "", ErrNotEligible
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM receipt_commands WHERE channel_id=? AND command_id=?`, channelID, commandID).Scan(&exists); err != nil {
		return "", err
	}
	if exists != 0 {
		return "duplicate", tx.Commit()
	}
	stamp := millis(serverNow)
	var userRecent, channelRecent int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM receipt_commands WHERE channel_id=? AND user_id=? AND status!='throttled' AND admitted_at>?`, channelID, userID, stamp-int64(30_000)).Scan(&userRecent); err != nil {
		return "", err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM receipt_commands WHERE channel_id=? AND status!='throttled' AND admitted_at>?`, channelID, stamp-int64(5_000)).Scan(&channelRecent); err != nil {
		return "", err
	}
	status := "processing"
	if userRecent > 0 || channelRecent >= 3 {
		status = "throttled"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO receipt_commands(channel_id,command_id,session_id,user_id,received_at,admitted_at,status) VALUES(?,?,?,?,?,?,?)`, channelID, commandID, sessionID, userID, millis(receivedAt), stamp, status); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return status, nil
}

func (s *Store) FinishReceiptCommand(ctx context.Context, channelID, commandID, status string) error {
	switch status {
	case "captured", "no_claim", "suppressed", "unavailable", "throttled":
	default:
		return ErrInvalid
	}
	res, err := s.db.Writer().ExecContext(ctx, `UPDATE receipt_commands SET status=? WHERE channel_id=? AND command_id=? AND status='processing'`, status, channelID, commandID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrConflict
	}
	return nil
}

// CaptureReceipt rechecks session ownership and the exact source rows inside
// one transaction, then persists text, evidence and independent clip work.
func (s *Store) CaptureReceipt(ctx context.Context, in receipt.CaptureInput) (receipt.Receipt, error) {
	if in.CommandID == "" || in.ChannelID == "" || in.SessionID == "" || in.UserID == "" || in.Login == "" || strings.TrimSpace(in.Claim) == "" || len(in.Evidence) == 0 {
		return receipt.Receipt{}, ErrInvalid
	}
	key, err := s.newID()
	if err != nil {
		return receipt.Receipt{}, err
	}
	clipKey, err := s.newID()
	if err != nil {
		return receipt.Receipt{}, err
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return receipt.Receipt{}, err
	}
	defer tx.Rollback()
	var live int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM live_sessions WHERE id=? AND channel_id=? AND state='live'`, in.SessionID, in.ChannelID).Scan(&live); err != nil {
		return receipt.Receipt{}, err
	}
	if live == 0 {
		return receipt.Receipt{}, ErrConflict
	}
	var commandStatus, commandSession string
	if err = tx.QueryRowContext(ctx, `SELECT status,session_id FROM receipt_commands WHERE channel_id=? AND command_id=? AND user_id=?`, in.ChannelID, in.CommandID, in.UserID).Scan(&commandStatus, &commandSession); errors.Is(err, sql.ErrNoRows) {
		return receipt.Receipt{}, ErrNotFound
	} else if err != nil {
		return receipt.Receipt{}, err
	}
	if commandSession != in.SessionID {
		return receipt.Receipt{}, ErrConflict
	}
	if commandStatus != "processing" {
		if commandStatus == "throttled" {
			return receipt.Receipt{}, receipt.ErrCommandThrottled
		}
		return receipt.Receipt{}, receipt.ErrCommandDuplicate
	}
	now := s.now().UTC()
	if in.ReceivedAt.IsZero() || in.ReceivedAt.After(now) || now.Sub(in.ReceivedAt) > 30*time.Second {
		return receipt.Receipt{}, ErrNotEligible
	}
	var startedMillis int64
	if err = tx.QueryRowContext(ctx, `SELECT started_at FROM live_sessions WHERE id=? AND channel_id=? AND state='live'`, in.SessionID, in.ChannelID).Scan(&startedMillis); err != nil {
		return receipt.Receipt{}, ErrConflict
	}
	var persistedReceived int64
	if err = tx.QueryRowContext(ctx, `SELECT received_at FROM receipt_commands WHERE channel_id=? AND command_id=? AND user_id=? AND session_id=? AND status='processing'`, in.ChannelID, in.CommandID, in.UserID, in.SessionID).Scan(&persistedReceived); errors.Is(err, sql.ErrNoRows) {
		return receipt.Receipt{}, ErrConflict
	} else if err != nil {
		return receipt.Receipt{}, err
	}
	if persistedReceived != millis(in.ReceivedAt) {
		return receipt.Receipt{}, ErrConflict
	}
	commandReceived := time.UnixMilli(persistedReceived).UTC()
	if commandReceived.After(now) || now.Sub(commandReceived) > 30*time.Second {
		return receipt.Receipt{}, ErrNotEligible
	}
	commandOffset := commandReceived.Sub(time.UnixMilli(startedMillis).UTC())
	if commandOffset < 0 || in.Window <= 0 {
		return receipt.Receipt{}, ErrNotEligible
	}
	cutoff := commandOffset - in.Window
	if cutoff < 0 {
		cutoff = 0
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO receipts(id,channel_id,session_id,created_at,claim,clipped_by,validity,sensitive) VALUES(?,?,?,?,?,?,'valid',0)`, key, in.ChannelID, in.SessionID, millis(now), in.Claim, in.Login)
	if err != nil {
		return receipt.Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO receipt_captures(receipt_id,channel_id,command_id,user_id,login) VALUES(?,?,?,?,?)`, key, in.ChannelID, in.CommandID, in.UserID, in.Login); err != nil {
		return receipt.Receipt{}, err
	}
	for _, e := range in.Evidence {
		if e.Start < cutoff || e.End > commandOffset {
			return receipt.Receipt{}, ErrNotEligible
		}
		if err = validateSource(ctx, tx, in.SessionID, e); err != nil {
			return receipt.Receipt{}, err
		}
		if e.Attribution != "broadcaster" {
			return receipt.Receipt{}, ErrNotEligible
		}
		eid, eerr := s.newID()
		if eerr != nil {
			return receipt.Receipt{}, eerr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO evidence(id,receipt_id,source_id,source_text,start_ms,end_ms,attribution,timing_basis) VALUES(?,?,?,?,?,?,?,COALESCE((SELECT timing_basis FROM transcript_segments WHERE id=?),'unspecified'))`, eid, key, e.SourceID, e.Text, e.Start.Milliseconds(), e.End.Milliseconds(), e.Attribution, e.SourceID); err != nil {
			return receipt.Receipt{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO clip_jobs(id,channel_id,receipt_id,requested_at,status) VALUES(?,?,?,?,'pending')`, clipKey, in.ChannelID, key, millis(now)); err != nil {
		return receipt.Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE receipt_commands SET status='captured' WHERE channel_id=? AND command_id=? AND status='processing'`, in.ChannelID, in.CommandID); err != nil {
		return receipt.Receipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return receipt.Receipt{}, err
	}
	return s.GetReceipt(ctx, key)
}

func (s *Store) CreateReceipt(ctx context.Context, in receipt.ReceiptInput) (receipt.Receipt, error) {
	if in.ChannelID == "" || in.SessionID == "" || in.Claim == "" || in.ClippedBy == "" || len(in.Evidence) == 0 {
		return receipt.Receipt{}, ErrInvalid
	}
	in.CreatedAt = s.now().UTC()
	if in.ExpiresAt != nil && !in.ExpiresAt.After(in.CreatedAt) {
		return receipt.Receipt{}, ErrInvalid
	}
	key, err := s.newID()
	if err != nil {
		return receipt.Receipt{}, err
	}
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return receipt.Receipt{}, err
	}
	defer tx.Rollback()
	var live int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM live_sessions WHERE id=? AND channel_id=? AND state='live'`, in.SessionID, in.ChannelID).Scan(&live); err != nil {
		return receipt.Receipt{}, err
	}
	if live == 0 {
		return receipt.Receipt{}, ErrConflict
	}
	var expires any
	if in.ExpiresAt != nil {
		expires = millis(*in.ExpiresAt)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO receipts(id,channel_id,session_id,created_at,expires_at,claim,clipped_by,validity,sensitive) VALUES(?,?,?,?,?,?,?,'valid',?)`, key, in.ChannelID, in.SessionID, millis(in.CreatedAt), expires, in.Claim, in.ClippedBy, in.Sensitive)
	if err != nil {
		return receipt.Receipt{}, err
	}
	for _, e := range in.Evidence {
		if err = validateSource(ctx, tx, in.SessionID, e); err != nil {
			return receipt.Receipt{}, err
		}
		if e.Attribution != "broadcaster" {
			return receipt.Receipt{}, ErrNotEligible
		}
		eid, eerr := s.newID()
		if eerr != nil {
			return receipt.Receipt{}, eerr
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO evidence(id,receipt_id,source_id,source_text,start_ms,end_ms,attribution,timing_basis) VALUES(?,?,?,?,?,?,?,COALESCE((SELECT timing_basis FROM transcript_segments WHERE id=?),'unspecified'))`, eid, key, e.SourceID, e.Text, e.Start.Milliseconds(), e.End.Milliseconds(), defaultText(e.Attribution, "uncertain"), e.SourceID)
		if err != nil {
			return receipt.Receipt{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return receipt.Receipt{}, err
	}
	return s.GetReceipt(ctx, key)
}

const receiptColumns = `id,channel_id,session_id,created_at,expires_at,claim,clipped_by,validity,sensitive,clip_status,clip_id,clip_url,verdict,version`

func scanReceipt(row *sql.Row) (receipt.Receipt, error) {
	var r receipt.Receipt
	var created int64
	var expires sql.NullInt64
	var sens int
	err := row.Scan(&r.ID, &r.ChannelID, &r.SessionID, &created, &expires, &r.Claim, &r.ClippedBy, &r.Validity, &sens, &r.ClipStatus, &r.ClipID, &r.ClipURL, &r.Verdict, &r.Version)
	if err != nil {
		return r, err
	}
	r.CreatedAt = time.UnixMilli(created).UTC()
	r.ExpiresAt = date(expires)
	r.Sensitive = sens != 0
	return r, nil
}
func (s *Store) GetReceipt(ctx context.Context, id string) (receipt.Receipt, error) {
	r, err := scanReceipt(s.db.Reader().QueryRowContext(ctx, `SELECT `+receiptColumns+` FROM receipts WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err == nil && r.Validity == receipt.Valid && r.ExpiresAt != nil && !r.ExpiresAt.After(s.now()) {
		r.Validity = receipt.Expired
	}
	return r, err
}

func (s *Store) ReceiptCredit(ctx context.Context, receiptID string) (receipt.Credit, error) {
	var credit receipt.Credit
	err := s.db.Reader().QueryRowContext(ctx, `SELECT user_id,login FROM receipt_captures WHERE receipt_id=?`, receiptID).Scan(&credit.UserID, &credit.Login)
	if errors.Is(err, sql.ErrNoRows) {
		return credit, ErrNotFound
	}
	return credit, err
}

func (s *Store) ClipJobForReceipt(ctx context.Context, channelID, receiptID string) (receipt.ClipJob, error) {
	var job receipt.ClipJob
	var requested int64
	var clipRequested, nextAttempt int64
	var createStarted int
	err := s.db.Reader().QueryRowContext(ctx, `SELECT j.id,j.channel_id,j.receipt_id,c.command_id,j.requested_at,j.status,j.clip_id,j.clip_url,j.last_error,j.attempts,j.clip_requested_at,j.next_attempt_at,j.create_started FROM clip_jobs j JOIN receipt_captures c ON c.receipt_id=j.receipt_id WHERE j.channel_id=? AND j.receipt_id=?`, channelID, receiptID).Scan(&job.ID, &job.ChannelID, &job.ReceiptID, &job.CommandID, &requested, &job.Status, &job.ClipID, &job.ClipURL, &job.LastError, &job.Attempts, &clipRequested, &nextAttempt, &createStarted)
	if errors.Is(err, sql.ErrNoRows) {
		return job, ErrNotFound
	}
	if err != nil {
		return job, err
	}
	job.RequestedAt = time.UnixMilli(requested).UTC()
	job.CreateStarted = createStarted != 0
	if clipRequested != 0 {
		job.ClipRequestedAt = time.UnixMilli(clipRequested).UTC()
	}
	if nextAttempt != 0 {
		job.NextAttemptAt = time.UnixMilli(nextAttempt).UTC()
	}
	return job, nil
}

// CompleteClipJob only lets the command that created the receipt change its
// clip outcome. Repeated identical results are safe; conflicting results fail.
func (s *Store) CompleteClipJob(ctx context.Context, channelID, commandID, status, clipID, clipURL, detail string) error {
	if channelID == "" || commandID == "" || (status != "ready" && status != "unavailable" && status != "failed") {
		return ErrInvalid
	}
	if status == "ready" && (clipID == "" || clipURL == "") {
		return ErrInvalid
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var jobID, receiptID, oldStatus, oldID, oldURL, oldDetail string
	err = tx.QueryRowContext(ctx, `SELECT j.id,j.receipt_id,j.status,j.clip_id,j.clip_url,j.last_error FROM clip_jobs j JOIN receipt_captures c ON c.receipt_id=j.receipt_id WHERE j.channel_id=? AND c.channel_id=? AND c.command_id=?`, channelID, channelID, commandID).Scan(&jobID, &receiptID, &oldStatus, &oldID, &oldURL, &oldDetail)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if oldStatus != "pending" {
		if oldStatus == status && oldID == clipID && oldURL == clipURL && oldDetail == detail {
			return tx.Commit()
		}
		return ErrConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE clip_jobs SET status=?,clip_id=?,clip_url=?,last_error=? WHERE id=? AND status='pending'`, status, clipID, clipURL, detail, jobID)
	if err != nil {
		return err
	}
	receiptStatus := receipt.ClipUnavailable
	if status == "ready" {
		receiptStatus = receipt.ClipReady
	}
	_, err = tx.ExecContext(ctx, `UPDATE receipts SET clip_status=?,clip_id=?,clip_url=?,version=version+1 WHERE id=? AND channel_id=?`, receiptStatus, clipID, clipURL, receiptID, channelID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Evidence(ctx context.Context, receiptID string) ([]receipt.Evidence, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT e.id,e.receipt_id,e.source_id,e.source_text,e.start_ms,e.end_ms,e.attribution,e.timing_basis FROM evidence e JOIN receipts r ON r.id=e.receipt_id WHERE e.receipt_id=? AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?) ORDER BY e.start_ms`, receiptID, millis(s.now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []receipt.Evidence{}
	for rows.Next() {
		var e receipt.Evidence
		var a, b int64
		if err = rows.Scan(&e.ID, &e.ReceiptID, &e.SourceID, &e.Text, &a, &b, &e.Attribution, &e.TimingBasis); err != nil {
			return nil, err
		}
		e.Start = time.Duration(a) * time.Millisecond
		e.End = time.Duration(b) * time.Millisecond
		out = append(out, e)
	}
	return out, rows.Err()
}

// PublicReceipts excludes void, expired and sensitive material. It is the
// shared retrieval surface for matching and lore, so callers cannot bypass it.
func (s *Store) PublicReceipts(ctx context.Context, channelID string, limit int) ([]receipt.Receipt, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT `+receiptColumns+` FROM receipts WHERE channel_id=? AND validity='valid' AND sensitive=0 AND (expires_at IS NULL OR expires_at>?) ORDER BY created_at DESC LIMIT ?`, channelID, millis(s.now()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []receipt.Receipt{}
	for rows.Next() {
		var r receipt.Receipt
		var created int64
		var expires sql.NullInt64
		var sensitive int
		if err = rows.Scan(&r.ID, &r.ChannelID, &r.SessionID, &created, &expires, &r.Claim, &r.ClippedBy, &r.Validity, &sensitive, &r.ClipStatus, &r.ClipID, &r.ClipURL, &r.Verdict, &r.Version); err != nil {
			return nil, err
		}
		r.CreatedAt = time.UnixMilli(created).UTC()
		r.ExpiresAt = date(expires)
		r.Sensitive = sensitive != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) SetClip(ctx context.Context, channelID, receiptID string, status receipt.ClipStatus, clipID, clipURL string) error {
	if status != receipt.ClipPending && status != receipt.ClipReady && status != receipt.ClipUnavailable {
		return ErrInvalid
	}
	if status == receipt.ClipReady && (clipID == "" || clipURL == "") {
		return ErrInvalid
	}
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current receipt.ClipStatus
	var currentID, currentURL string
	err = tx.QueryRowContext(ctx, `SELECT clip_status,clip_id,clip_url FROM receipts WHERE id=? AND channel_id=?`, receiptID, channelID).Scan(&current, &currentID, &currentURL)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if current == status && currentID == clipID && currentURL == clipURL {
		return tx.Commit()
	}
	if current != receipt.ClipPending || status == receipt.ClipPending {
		return ErrConflict
	}
	res, err := tx.ExecContext(ctx, `UPDATE receipts SET clip_status=?,clip_id=?,clip_url=?,version=version+1 WHERE id=? AND channel_id=?`, status, clipID, clipURL, receiptID, channelID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) Void(ctx context.Context, channelID, receiptID string) error {
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE receipts SET validity='void',version=version+1 WHERE id=? AND channel_id=? AND validity='valid'`, receiptID, channelID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM receipts WHERE id=? AND channel_id=?`, receiptID, channelID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		return tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `UPDATE reviews SET state='rejected',decided_at=? WHERE detection_id IN (SELECT id FROM detections WHERE receipt_id=?) AND state='open'`, millis(s.now()), receiptID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE appeals SET state='closed',decision='void',decided_at=? WHERE receipt_id=? AND state='open'`, millis(s.now()), receiptID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RecordDetection deduplicates source spans per receipt. A stamp is only
// accepted for a valid, unexpired, non-sensitive receipt in the same channel.
func (s *Store) RecordDetection(ctx context.Context, in receipt.DetectionInput) (receipt.Detection, error) {
	e := in.Evidence
	if in.ChannelID == "" || in.ReceiptID == "" || e.SourceID == "" || e.Text == "" || e.Start < 0 || e.End < e.Start {
		return receipt.Detection{}, ErrInvalid
	}
	if in.Outcome != receipt.NeedsReview && in.Outcome != receipt.Stamp && in.Outcome != receipt.Dismissed {
		return receipt.Detection{}, ErrInvalid
	}
	key, err := s.newID()
	if err != nil {
		return receipt.Detection{}, err
	}
	now := s.now().UTC()
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return receipt.Detection{}, err
	}
	defer tx.Rollback()
	var eligible int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM receipts WHERE id=? AND channel_id=? AND validity='valid' AND sensitive=0 AND (expires_at IS NULL OR expires_at>?)`, in.ReceiptID, in.ChannelID, millis(now)).Scan(&eligible)
	if err != nil {
		return receipt.Detection{}, err
	}
	if eligible == 0 {
		return receipt.Detection{}, ErrNotEligible
	}
	// First honor a durable retry even if its source session has since ended.
	var d receipt.Detection
	var a, b, created int64
	err = tx.QueryRowContext(ctx, `SELECT id,channel_id,receipt_id,session_id,source_id,start_ms,end_ms,evidence_text,attribution,outcome,created_at FROM detections WHERE receipt_id=? AND source_id=? AND start_ms=? AND end_ms=?`, in.ReceiptID, e.SourceID, e.Start.Milliseconds(), e.End.Milliseconds()).Scan(&d.ID, &d.ChannelID, &d.ReceiptID, &d.SessionID, &d.SourceID, &a, &b, &d.EvidenceText, &d.Attribution, &d.Outcome, &created)
	if err == nil {
		compatible := d.Outcome == in.Outcome || (in.Outcome == receipt.NeedsReview && (d.Outcome == receipt.Stamp || d.Outcome == receipt.Dismissed))
		if d.ChannelID != in.ChannelID || d.EvidenceText != e.Text || d.Attribution != defaultText(e.Attribution, "uncertain") || !compatible {
			return d, ErrConflict
		}
		d.Start = time.Duration(a) * time.Millisecond
		d.End = time.Duration(b) * time.Millisecond
		d.CreatedAt = time.UnixMilli(created).UTC()
		if err = tx.Commit(); err != nil {
			return d, err
		}
		return d, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return d, err
	}
	var sessionID string
	err = tx.QueryRowContext(ctx, `SELECT s.id FROM receipts r JOIN transcript_segments t ON t.id=? JOIN live_sessions s ON s.id=t.session_id AND s.channel_id=r.channel_id AND s.state='live' WHERE r.id=? AND r.channel_id=?`, e.SourceID, in.ReceiptID, in.ChannelID).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt.Detection{}, ErrNotEligible
	}
	if err != nil {
		return receipt.Detection{}, err
	}
	if err = validateSource(ctx, tx, sessionID, e); err != nil {
		return receipt.Detection{}, err
	}
	if in.Outcome == receipt.Stamp && e.Attribution != "broadcaster" {
		return receipt.Detection{}, ErrNotEligible
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO detections(id,channel_id,receipt_id,session_id,source_id,start_ms,end_ms,evidence_text,attribution,outcome,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(receipt_id,source_id,start_ms,end_ms) DO NOTHING`, key, in.ChannelID, in.ReceiptID, sessionID, e.SourceID, e.Start.Milliseconds(), e.End.Milliseconds(), e.Text, defaultText(e.Attribution, "uncertain"), in.Outcome, millis(now))
	if err != nil {
		return receipt.Detection{}, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id,channel_id,receipt_id,session_id,source_id,start_ms,end_ms,evidence_text,attribution,outcome,created_at FROM detections WHERE receipt_id=? AND source_id=? AND start_ms=? AND end_ms=?`, in.ReceiptID, e.SourceID, e.Start.Milliseconds(), e.End.Milliseconds()).Scan(&d.ID, &d.ChannelID, &d.ReceiptID, &d.SessionID, &d.SourceID, &a, &b, &d.EvidenceText, &d.Attribution, &d.Outcome, &created)
	if err != nil {
		return d, err
	}
	compatibleOutcome := d.Outcome == in.Outcome || (in.Outcome == receipt.NeedsReview && (d.Outcome == receipt.Stamp || d.Outcome == receipt.Dismissed))
	if d.EvidenceText != e.Text || d.Attribution != defaultText(e.Attribution, "uncertain") || !compatibleOutcome {
		return d, ErrConflict
	}
	d.Start = time.Duration(a) * time.Millisecond
	d.End = time.Duration(b) * time.Millisecond
	d.CreatedAt = time.UnixMilli(created).UTC()
	if d.Outcome == receipt.NeedsReview {
		_, err = tx.ExecContext(ctx, `INSERT INTO reviews(id,detection_id,state,created_at) VALUES(?,?,'open',?) ON CONFLICT(detection_id) DO NOTHING`, key+"r", d.ID, millis(now))
		if err != nil {
			return d, err
		}
	}
	if err = tx.Commit(); err != nil {
		return d, err
	}
	return d, nil
}

// DecideReview is an idempotent mod decision. Repeating the same decision is
// safe; reversing a completed decision is a conflict requiring a new workflow.
func (s *Store) DecideReview(ctx context.Context, channelID, detectionID string, approve bool) (receipt.Review, error) {
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return receipt.Review{}, err
	}
	defer tx.Rollback()
	var r receipt.Review
	var created int64
	var decided sql.NullInt64
	var state string
	err = tx.QueryRowContext(ctx, `SELECT v.id,v.detection_id,v.state,v.created_at,v.decided_at FROM reviews v JOIN detections d ON d.id=v.detection_id WHERE d.id=? AND d.channel_id=?`, detectionID, channelID).Scan(&r.ID, &r.DetectionID, &state, &created, &decided)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	var actualChannel string
	err = tx.QueryRowContext(ctx, `SELECT channel_id FROM detections WHERE id=?`, detectionID).Scan(&actualChannel)
	if err != nil {
		return r, err
	}
	if actualChannel != channelID {
		return r, ErrNotFound
	}
	want := "rejected"
	outcome := "dismissed"
	if approve {
		want = "approved"
		outcome = "stamp"
		if state != "approved" {
			var eligible int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM detections d JOIN receipts r ON r.id=d.receipt_id WHERE d.id=? AND d.channel_id=? AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?)`, detectionID, channelID, millis(s.now())).Scan(&eligible); err != nil {
				return r, err
			}
			if eligible == 0 {
				return r, ErrNotEligible
			}
		}
	}
	if state != "open" && state != want {
		return r, ErrConflict
	}
	if state == "open" {
		if approve {
			var attribution string
			if err = tx.QueryRowContext(ctx, `SELECT attribution FROM detections WHERE id=?`, detectionID).Scan(&attribution); err != nil {
				return r, err
			}
			if attribution != "broadcaster" {
				return r, ErrNotEligible
			}
		}
		now := millis(s.now())
		_, err = tx.ExecContext(ctx, `UPDATE reviews SET state=?,decided_at=? WHERE detection_id=? AND state='open'`, want, now, detectionID)
		if err != nil {
			return r, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE detections SET outcome=? WHERE id=?`, outcome, detectionID)
		if err != nil {
			return r, err
		}
		state = want
		decided = sql.NullInt64{Int64: now, Valid: true}
	}
	if err = tx.Commit(); err != nil {
		return r, err
	}
	r.State = state
	r.CreatedAt = time.UnixMilli(created).UTC()
	r.DecidedAt = date(decided)
	return r, nil
}

func (s *Store) OpenAppeal(ctx context.Context, channelID, receiptID string, duration time.Duration) (receipt.Appeal, error) {
	if duration <= 0 {
		return receipt.Appeal{}, ErrInvalid
	}
	now := s.now().UTC()
	key, err := s.newID()
	if err != nil {
		return receipt.Appeal{}, err
	}
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return receipt.Appeal{}, err
	}
	defer tx.Rollback()
	var eligible int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM receipts WHERE id=? AND channel_id=? AND validity='valid' AND sensitive=0 AND (expires_at IS NULL OR expires_at>?)`, receiptID, channelID, millis(now)).Scan(&eligible)
	if err != nil {
		return receipt.Appeal{}, err
	}
	if eligible == 0 {
		return receipt.Appeal{}, ErrNotEligible
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO appeals(id,receipt_id,state,opened_at,closes_at) VALUES(?,?,'open',?,?) ON CONFLICT DO NOTHING`, key, receiptID, millis(now), millis(now.Add(duration)))
	if err != nil {
		return receipt.Appeal{}, err
	}
	var a receipt.Appeal
	var opened, closes int64
	var decided sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id,receipt_id,state,opened_at,closes_at,decision,decided_at FROM appeals WHERE receipt_id=? AND state='open'`, receiptID).Scan(&a.ID, &a.ReceiptID, &a.State, &opened, &closes, &a.Decision, &decided)
	if err != nil {
		return a, err
	}
	if err = tx.Commit(); err != nil {
		return a, err
	}
	a.OpenedAt = time.UnixMilli(opened).UTC()
	a.ClosesAt = time.UnixMilli(closes).UTC()
	a.DecidedAt = date(decided)
	return a, nil
}

// CastVote inserts one immutable vote per Twitch voter and appeal. A duplicate
// with the same choice is idempotent; a changed choice is rejected.
func (s *Store) CastVote(ctx context.Context, channelID, appealID, voterID string, choice receipt.VoteChoice) (receipt.Vote, error) {
	if voterID == "" || (choice != receipt.VoteUphold && choice != receipt.VoteOverturn) {
		return receipt.Vote{}, ErrInvalid
	}
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return receipt.Vote{}, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	var valid int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM appeals a JOIN receipts r ON r.id=a.receipt_id WHERE a.id=? AND r.channel_id=? AND a.state='open' AND a.closes_at>? AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?)`, appealID, channelID, millis(now), millis(now)).Scan(&valid)
	if err != nil {
		return receipt.Vote{}, err
	}
	if valid == 0 {
		return receipt.Vote{}, ErrNotEligible
	}
	key, err := s.newID()
	if err != nil {
		return receipt.Vote{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO votes(id,appeal_id,voter_id,choice,created_at) VALUES(?,?,?,?,?) ON CONFLICT(appeal_id,voter_id) DO NOTHING`, key, appealID, voterID, choice, millis(now))
	if err != nil {
		return receipt.Vote{}, err
	}
	var v receipt.Vote
	var created int64
	err = tx.QueryRowContext(ctx, `SELECT id,appeal_id,voter_id,choice,created_at FROM votes WHERE appeal_id=? AND voter_id=?`, appealID, voterID).Scan(&v.ID, &v.AppealID, &v.VoterID, &v.Choice, &created)
	if err != nil {
		return v, err
	}
	if v.Choice != choice {
		return v, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return v, err
	}
	v.CreatedAt = time.UnixMilli(created).UTC()
	return v, nil
}

// Decide records Admit or Context changed with a caller-supplied stable key.
// The unique key makes retries safe after a timeout or process restart.
func (s *Store) Decide(ctx context.Context, channelID, receiptID, decisionKey string, outcome receipt.Verdict, note string) (receipt.VerdictRecord, error) {
	if decisionKey == "" || (outcome != receipt.Admitted && outcome != receipt.ContextChanged) {
		return receipt.VerdictRecord{}, ErrInvalid
	}
	return s.insertVerdict(ctx, channelID, receiptID, "", decisionKey, outcome, note)
}

func (s *Store) FinalizeAppeal(ctx context.Context, channelID, appealID, decisionKey string) (receipt.VerdictRecord, error) {
	if decisionKey == "" {
		return receipt.VerdictRecord{}, ErrInvalid
	}
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return receipt.VerdictRecord{}, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	// A retried close returns the first durable decision, even though the appeal
	// is already closed. Reusing its key with another appeal is rejected.
	var appealReceiptID string
	if err = tx.QueryRowContext(ctx, `SELECT r.id FROM appeals a JOIN receipts r ON r.id=a.receipt_id WHERE a.id=? AND r.channel_id=?`, appealID, channelID).Scan(&appealReceiptID); errors.Is(err, sql.ErrNoRows) {
		return receipt.VerdictRecord{}, ErrNotFound
	} else if err != nil {
		return receipt.VerdictRecord{}, err
	}
	if existing, err := verdictByKey(ctx, tx, appealReceiptID, decisionKey); err == nil {
		if existing.AppealID != appealID {
			return receipt.VerdictRecord{}, ErrConflict
		}
		if err = tx.Commit(); err != nil {
			return receipt.VerdictRecord{}, err
		}
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return receipt.VerdictRecord{}, err
	}
	var receiptID, state string
	var closes int64
	var expires sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT a.receipt_id,a.state,a.closes_at,r.expires_at FROM appeals a JOIN receipts r ON r.id=a.receipt_id WHERE a.id=? AND r.channel_id=? AND r.validity='valid' AND r.sensitive=0`, appealID, channelID).Scan(&receiptID, &state, &closes, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt.VerdictRecord{}, ErrNotFound
	}
	if err != nil {
		return receipt.VerdictRecord{}, err
	}
	if expires.Valid && expires.Int64 <= millis(now) {
		return receipt.VerdictRecord{}, ErrNotEligible
	}
	if state != "open" {
		return receipt.VerdictRecord{}, ErrConflict
	}
	if millis(now) < closes {
		return receipt.VerdictRecord{}, ErrNotEligible
	}
	var uphold, overturn int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE choice='uphold'),count(*) FILTER (WHERE choice='overturn') FROM votes WHERE appeal_id=?`, appealID).Scan(&uphold, &overturn)
	if err != nil {
		return receipt.VerdictRecord{}, err
	}
	outcome := receipt.Inconclusive
	if uphold > overturn {
		outcome = receipt.Upheld
	} else if overturn > uphold {
		outcome = receipt.Overturned
	}
	key, err := s.newID()
	if err != nil {
		return receipt.VerdictRecord{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE appeals SET state='closed',decision=?,decided_at=? WHERE id=? AND state='open'`, outcome, millis(now), appealID)
	if err != nil {
		return receipt.VerdictRecord{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE receipts SET verdict=?,version=version+1 WHERE id=?`, outcome, receiptID)
	if err != nil {
		return receipt.VerdictRecord{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO verdicts(id,receipt_id,appeal_id,decision_key,outcome,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(receipt_id,decision_key) DO NOTHING`, key, receiptID, appealID, decisionKey, outcome, millis(now))
	if err != nil {
		return receipt.VerdictRecord{}, err
	}
	if err = tx.Commit(); err != nil {
		return receipt.VerdictRecord{}, err
	}
	return s.getVerdict(ctx, receiptID, decisionKey)
}

func (s *Store) insertVerdict(ctx context.Context, channelID, receiptID, appealID, decisionKey string, outcome receipt.Verdict, note string) (receipt.VerdictRecord, error) {
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		return receipt.VerdictRecord{}, err
	}
	defer tx.Rollback()
	if existing, err := verdictByKey(ctx, tx, receiptID, decisionKey); err == nil {
		var owner string
		if err = tx.QueryRowContext(ctx, `SELECT channel_id FROM receipts WHERE id=?`, existing.ReceiptID).Scan(&owner); err != nil || existing.ReceiptID != receiptID || owner != channelID {
			return receipt.VerdictRecord{}, ErrConflict
		}
		if existing.Outcome != outcome || existing.Note != note {
			return receipt.VerdictRecord{}, ErrConflict
		}
		if err = tx.Commit(); err != nil {
			return receipt.VerdictRecord{}, err
		}
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return receipt.VerdictRecord{}, err
	}
	var valid int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM receipts WHERE id=? AND channel_id=? AND validity='valid' AND sensitive=0 AND (expires_at IS NULL OR expires_at>?)`, receiptID, channelID, millis(s.now())).Scan(&valid)
	if err != nil {
		return receipt.VerdictRecord{}, err
	}
	if valid == 0 {
		return receipt.VerdictRecord{}, ErrNotEligible
	}
	key, err := s.newID()
	if err != nil {
		return receipt.VerdictRecord{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO verdicts(id,receipt_id,appeal_id,decision_key,outcome,note,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(receipt_id,decision_key) DO NOTHING`, key, receiptID, nullText(appealID), decisionKey, outcome, note, millis(s.now()))
	if err != nil {
		return receipt.VerdictRecord{}, err
	}
	var existing receipt.VerdictRecord
	var aid sql.NullString
	var created int64
	err = tx.QueryRowContext(ctx, `SELECT id,receipt_id,appeal_id,decision_key,outcome,note,created_at FROM verdicts WHERE receipt_id=? AND decision_key=?`, receiptID, decisionKey).Scan(&existing.ID, &existing.ReceiptID, &aid, &existing.DecisionKey, &existing.Outcome, &existing.Note, &created)
	if err != nil {
		return existing, err
	}
	if existing.Outcome != outcome || existing.Note != note {
		return existing, ErrConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE receipts SET verdict=?,version=version+1 WHERE id=?`, outcome, receiptID)
	if err != nil {
		return existing, err
	}
	if err = tx.Commit(); err != nil {
		return existing, err
	}
	existing.AppealID = strNull(aid)
	existing.CreatedAt = time.UnixMilli(created).UTC()
	return existing, nil
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func verdictByKey(ctx context.Context, tx *sql.Tx, receiptID, key string) (receipt.VerdictRecord, error) {
	var r receipt.VerdictRecord
	var appeal sql.NullString
	var created int64
	err := tx.QueryRowContext(ctx, `SELECT id,receipt_id,appeal_id,decision_key,outcome,note,created_at FROM verdicts WHERE receipt_id=? AND decision_key=?`, receiptID, key).Scan(&r.ID, &r.ReceiptID, &appeal, &r.DecisionKey, &r.Outcome, &r.Note, &created)
	if err != nil {
		return r, err
	}
	r.AppealID = strNull(appeal)
	r.CreatedAt = time.UnixMilli(created).UTC()
	return r, nil
}
func (s *Store) getVerdict(ctx context.Context, receiptID, key string) (receipt.VerdictRecord, error) {
	var r receipt.VerdictRecord
	var appeal sql.NullString
	var created int64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT id,receipt_id,appeal_id,decision_key,outcome,note,created_at FROM verdicts WHERE receipt_id=? AND decision_key=?`, receiptID, key).Scan(&r.ID, &r.ReceiptID, &appeal, &r.DecisionKey, &r.Outcome, &r.Note, &created)
	if err != nil {
		return r, err
	}
	r.AppealID = strNull(appeal)
	r.CreatedAt = time.UnixMilli(created).UTC()
	return r, nil
}

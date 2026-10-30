package store

import (
	"context"
	"strings"
	"time"

	"receipts/internal/receipt"
)

// AddLiveSegment commits only against the exact active owner and generation.
// Overlapping time ranges are rejected, including after process restart.
func (s *Store) AddLiveSegment(ctx context.Context, accountID, channelID, sessionID string, generation uint64, seg receipt.Segment) error {
	if accountID == "" || channelID == "" || sessionID == "" || generation == 0 || seg.SessionID != sessionID || seg.Start < 0 || seg.End <= seg.Start || strings.TrimSpace(seg.Text) == "" {
		return ErrInvalid
	}
	if seg.TimingBasis != "coarse_pcm_ingest_hls_delay_unknown" || (seg.Attribution != "uncertain" && seg.Attribution != "broadcaster") {
		return ErrInvalid
	}
	if seg.ID == "" {
		var err error
		seg.ID, err = s.newID()
		if err != nil {
			return err
		}
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM live_sessions l JOIN accounts a ON a.channel_id=l.channel_id WHERE a.id=? AND a.channel_id=? AND a.status='connected' AND l.id=? AND l.state='live' AND l.ended_at IS NULL AND l.runtime_generation=?`, accountID, channelID, sessionID, generation).Scan(&active)
	if err != nil {
		return err
	}
	if active != 1 {
		return ErrConflict
	}
	var overlap int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM transcript_segments WHERE session_id=? AND start_ms < ? AND end_ms > ?`, sessionID, seg.End.Milliseconds(), seg.Start.Milliseconds()).Scan(&overlap); err != nil {
		return err
	}
	if overlap != 0 {
		return ErrConflict
	}
	now := s.now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO transcript_segments(id,session_id,start_ms,end_ms,text,speaker_id,attribution,created_at,timing_basis) VALUES(?,?,?,?,?,?,?,?,?)`, seg.ID, seg.SessionID, seg.Start.Milliseconds(), seg.End.Milliseconds(), seg.Text, seg.SpeakerID, seg.Attribution, millis(now), seg.TimingBasis)
	if err != nil {
		return err
	}
	if s.retention.ExpireAfter > 0 {
		if _, err = tx.ExecContext(ctx, `DELETE FROM transcript_segments WHERE session_id=? AND created_at < ?`, seg.SessionID, millis(now.Add(-s.retention.ExpireAfter))); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM transcript_segments WHERE session_id=? AND id NOT IN (SELECT id FROM transcript_segments WHERE session_id=? ORDER BY created_at DESC,start_ms DESC LIMIT ?)`, seg.SessionID, seg.SessionID, s.retention.MaxSegments); err != nil {
		return err
	}
	return tx.Commit()
}

// ClaimAudioChunk durably prevents repeated paid submissions, including after
// an ambiguous provider failure or a same-session runtime restart.
func (s *Store) ClaimAudioChunk(ctx context.Context, accountID, channelID, sessionID string, generation uint64, start, end time.Duration) error {
	if accountID == "" || channelID == "" || sessionID == "" || generation == 0 || start < 0 || end.Milliseconds() <= start.Milliseconds() {
		return ErrInvalid
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active, overlap int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM live_sessions l JOIN accounts a ON a.channel_id=l.channel_id WHERE a.id=? AND a.channel_id=? AND a.status='connected' AND l.id=? AND l.state='live' AND l.ended_at IS NULL AND l.runtime_generation=?`, accountID, channelID, sessionID, generation).Scan(&active); err != nil {
		return err
	}
	if active != 1 {
		return ErrConflict
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM audio_chunks WHERE session_id=? AND start_ms < ? AND end_ms > ?`, sessionID, end.Milliseconds(), start.Milliseconds()).Scan(&overlap); err != nil {
		return err
	}
	if overlap != 0 {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audio_chunks(session_id,start_ms,end_ms,generation) VALUES(?,?,?,?)`, sessionID, start.Milliseconds(), end.Milliseconds(), generation); err != nil {
		return err
	}
	return tx.Commit()
}

// LiveAudioSource returns the owner-verified Twitch login and persisted clock origin.
func (s *Store) LiveAudioSource(ctx context.Context, accountID, channelID, sessionID string, generation uint64) (string, time.Time, error) {
	var login string
	var started int64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT c.login,l.started_at FROM live_sessions l JOIN channels c ON c.id=l.channel_id JOIN accounts a ON a.channel_id=c.id WHERE a.id=? AND a.channel_id=? AND a.status='connected' AND a.provider='twitch' AND a.provider_subject=c.platform_id AND l.id=? AND l.state='live' AND l.ended_at IS NULL AND l.runtime_generation=?`, accountID, channelID, sessionID, generation).Scan(&login, &started)
	if err != nil {
		return "", time.Time{}, ErrConflict
	}
	return login, time.UnixMilli(started).UTC(), nil
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"receipts/internal/events"
	"receipts/internal/receipt"
)

// ValidateDetectionSource prevents fabricated or foreign transcript data from
// reaching the judge. ApplyDetection revalidates under the final write lock.
func (s *Store) ValidateDetectionSource(ctx context.Context, a DetectionAuthority, source receipt.Segment) error {
	if a.AccountID == "" || a.ChannelID == "" || a.LiveSessionID == "" || a.Generation == 0 || source.ID == "" || source.SessionID != a.LiveSessionID || source.Attribution != "broadcaster" || source.Text == "" || source.Start < 0 || source.End < source.Start {
		return ErrNotEligible
	}
	var count int
	err := s.db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM transcript_segments t JOIN live_sessions l ON l.id=t.session_id JOIN accounts a ON a.channel_id=l.channel_id WHERE a.id=? AND a.status='connected' AND l.channel_id=? AND l.id=? AND l.state='live' AND l.ended_at IS NULL AND (l.runtime_generation=? OR (l.runtime_generation=0 AND l.broadcast_id='')) AND t.id=? AND t.start_ms=? AND t.end_ms=? AND t.text=? AND t.attribution='broadcaster' AND t.speaker_id=? AND l.started_at+t.end_ms<=?`, a.AccountID, a.ChannelID, a.LiveSessionID, a.Generation, source.ID, source.Start.Milliseconds(), source.End.Milliseconds(), source.Text, source.SpeakerID, millis(s.now())).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotEligible
	}
	return nil
}

// ValidateJudgmentPair checks persisted chronology and eligibility before paid
// judging. A later line must begin after both receipt creation and the end of
// its original evidence, including when the receipt is from an earlier session.
func (s *Store) ValidateJudgmentPair(ctx context.Context, a DetectionAuthority, candidate receipt.Receipt, evidence receipt.Evidence, source receipt.Segment) error {
	if source.ID == "" || evidence.SourceID == "" || source.ID == evidence.SourceID || candidate.ID == "" || evidence.ReceiptID != candidate.ID {
		return ErrNotEligible
	}
	var count int
	err := s.db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM receipts r
 JOIN evidence e ON e.receipt_id=r.id
 JOIN live_sessions original ON original.id=r.session_id AND original.channel_id=r.channel_id
 JOIN live_sessions current ON current.id=? AND current.channel_id=r.channel_id
 JOIN accounts a ON a.channel_id=r.channel_id
 JOIN transcript_segments t ON t.id=? AND t.session_id=current.id
 WHERE a.id=? AND a.status='connected' AND r.channel_id=? AND r.id=? AND r.version=? AND r.claim=?
 AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?)
 AND current.state='live' AND current.ended_at IS NULL AND (current.runtime_generation=? OR (current.runtime_generation=0 AND current.broadcast_id=''))
 AND e.id=? AND e.source_id=? AND e.source_text=? AND e.start_ms=? AND e.end_ms=? AND e.attribution='broadcaster'
 AND t.start_ms=? AND t.end_ms=? AND t.text=? AND t.attribution='broadcaster' AND t.speaker_id=?
 AND t.id<>e.source_id AND current.started_at+t.start_ms>=r.created_at
 AND current.started_at+t.start_ms>=original.started_at+e.end_ms`, a.LiveSessionID, source.ID, a.AccountID, a.ChannelID, candidate.ID, candidate.Version, candidate.Claim, millis(s.now()), a.Generation, evidence.ID, evidence.SourceID, evidence.Text, evidence.Start.Milliseconds(), evidence.End.Milliseconds(), source.Start.Milliseconds(), source.End.Milliseconds(), source.Text, source.SpeakerID).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotEligible
	}
	return nil
}

// ApplyDetection rechecks every durable authority and source row under Keel's
// serialized writer. The caller serializes runtime authority and publication.
type DetectionAuthority struct {
	AccountID, ChannelID, LiveSessionID string
	Generation                          uint64
}

func (s *Store) ApplyDetection(ctx context.Context, a DetectionAuthority, candidate receipt.Receipt, source receipt.Segment, outcome receipt.DetectionOutcome, cooldown time.Duration) (*events.PublicReceipt, error) {
	if a.AccountID == "" || a.ChannelID == "" || a.LiveSessionID == "" || a.Generation == 0 || candidate.ID == "" || source.ID == "" || source.SessionID != a.LiveSessionID || source.Attribution != "broadcaster" || source.Text == "" || source.Start < 0 || source.End < source.Start {
		return nil, ErrInvalid
	}
	if outcome != receipt.Stamp && outcome != receipt.NeedsReview && outcome != receipt.Dismissed {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	var claim, clipURL, clipStatus, verdict, clippedBy string
	var created, sessionStart int64
	err = tx.QueryRowContext(ctx, `SELECT r.claim,r.clip_url,r.clip_status,r.verdict,r.clipped_by,r.created_at,l.started_at FROM receipts r JOIN accounts a ON a.channel_id=r.channel_id JOIN live_sessions l ON l.channel_id=r.channel_id WHERE a.id=? AND a.status='connected' AND r.channel_id=? AND r.id=? AND r.version=? AND r.claim=? AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?) AND l.id=? AND l.state='live' AND l.ended_at IS NULL AND (l.runtime_generation=? OR (l.runtime_generation=0 AND l.broadcast_id='')) AND r.created_at <= l.started_at + ?`, a.AccountID, a.ChannelID, candidate.ID, candidate.Version, candidate.Claim, millis(now), a.LiveSessionID, a.Generation, source.End.Milliseconds()).Scan(&claim, &clipURL, &clipStatus, &verdict, &clippedBy, &created, &sessionStart)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotEligible
	}
	if err != nil {
		return nil, err
	}
	e := receipt.EvidenceInput{SourceID: source.ID, Text: source.Text, Attribution: source.Attribution, Start: source.Start, End: source.End}
	if err = validateSource(ctx, tx, a.LiveSessionID, e); err != nil {
		return nil, err
	}
	// Source offsets and text must match the complete persisted segment.
	var exact int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM transcript_segments WHERE id=? AND session_id=? AND start_ms=? AND end_ms=? AND text=? AND attribution='broadcaster' AND speaker_id=?`, source.ID, a.LiveSessionID, source.Start.Milliseconds(), source.End.Milliseconds(), source.Text, source.SpeakerID).Scan(&exact); err != nil {
		return nil, err
	}
	if exact != 1 || sessionStart+source.End.Milliseconds() > millis(now) {
		return nil, ErrNotEligible
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM detections WHERE receipt_id=? AND source_id=? AND start_ms=? AND end_ms=?`, candidate.ID, source.ID, source.Start.Milliseconds(), source.End.Milliseconds()).Scan(&exists); err != nil {
		return nil, err
	}
	if exists != 0 {
		return nil, ErrConflict
	}
	if outcome == receipt.Stamp {
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM stamp_history WHERE channel_id=? AND stamped_at>?`, a.ChannelID, millis(now.Add(-cooldown))).Scan(&exists); err != nil {
			return nil, err
		}
		if exists != 0 {
			return nil, ErrNotEligible
		}
	}
	key, err := s.newID()
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO detections(id,channel_id,receipt_id,session_id,source_id,start_ms,end_ms,evidence_text,attribution,outcome,created_at,timing_basis) VALUES(?,?,?,?,?,?,?,?,?,?,?,COALESCE((SELECT timing_basis FROM transcript_segments WHERE id=?),'unspecified'))`, key, a.ChannelID, candidate.ID, a.LiveSessionID, source.ID, source.Start.Milliseconds(), source.End.Milliseconds(), source.Text, source.Attribution, outcome, millis(now), source.ID)
	if err != nil {
		return nil, err
	}
	if outcome == receipt.Stamp {
		if _, err = tx.ExecContext(ctx, `INSERT INTO stamp_history(detection_id,channel_id,stamped_at) VALUES(?,?,?)`, key, a.ChannelID, millis(now)); err != nil {
			return nil, err
		}
	}
	if outcome == receipt.NeedsReview {
		if _, err = tx.ExecContext(ctx, `INSERT INTO reviews(id,detection_id,state,created_at) VALUES(?,?,'open',?)`, key+"r", key, millis(now)); err != nil {
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &events.PublicReceipt{ID: candidate.ID, Claim: claim, ClippedBy: clippedBy, ClipURL: clipURL, ClipStatus: clipStatus, Verdict: verdict, CreatedAt: time.UnixMilli(created).UTC()}, nil
}

// CurrentStamp is state, not a new animation. Invalidated receipts disappear.
func (s *Store) CurrentStamp(ctx context.Context, channelID, sessionID string) (*events.PublicReceipt, error) {
	var p events.PublicReceipt
	var created int64
	err := s.db.Reader().QueryRowContext(ctx, `SELECT r.id,r.claim,r.clip_url,r.clip_status,r.verdict,r.clipped_by,r.created_at FROM (SELECT d.receipt_id FROM detections d LEFT JOIN stamp_history h ON h.detection_id=d.id WHERE d.channel_id=? AND d.session_id=? AND d.outcome='stamp' ORDER BY coalesce(h.stamped_at,d.created_at) DESC,d.id DESC LIMIT 1) latest JOIN receipts r ON r.id=latest.receipt_id WHERE r.channel_id=? AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?)`, channelID, sessionID, channelID, millis(s.now())).Scan(&p.ID, &p.Claim, &p.ClipURL, &p.ClipStatus, &p.Verdict, &p.ClippedBy, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt = time.UnixMilli(created).UTC()
	return &p, nil
}

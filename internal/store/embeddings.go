package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"receipts/internal/receipt"
	"receipts/internal/retrieval"
)

// ReindexEmbeddings atomically replaces the entire eligible channel index. The
// caller computes vectors outside the transaction; each claim and authority is
// checked again here, including coverage, before replacing the previous space.
func (s *Store) ReindexEmbeddings(ctx context.Context, a DetectionAuthority, space retrieval.Space, entries []retrieval.Entry) error {
	if a.AccountID == "" || a.ChannelID == "" || a.LiveSessionID == "" || a.Generation == 0 || space.Model == "" || space.Version == "" || space.Dimensions <= 0 {
		return ErrInvalid
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active, count int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM live_sessions l JOIN accounts a ON a.channel_id=l.channel_id WHERE a.id=? AND a.status='connected' AND l.channel_id=? AND l.id=? AND l.state='live' AND l.ended_at IS NULL AND l.runtime_generation=?`, a.AccountID, a.ChannelID, a.LiveSessionID, a.Generation).Scan(&active)
	if err != nil {
		return err
	}
	if active != 1 {
		return ErrNotEligible
	}
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM receipts WHERE channel_id=? AND validity='valid' AND sensitive=0 AND (expires_at IS NULL OR expires_at>?)`, a.ChannelID, millis(s.now())).Scan(&count)
	if err != nil {
		return err
	}
	if count != len(entries) {
		return ErrConflict
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.Receipt.ID] || !retrieval.ValidVector(e.Vector, space.Dimensions) {
			return ErrInvalid
		}
		seen[e.Receipt.ID] = true
		var n int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM receipts WHERE id=? AND channel_id=? AND claim=? AND validity='valid' AND sensitive=0 AND (expires_at IS NULL OR expires_at>?)`, e.Receipt.ID, a.ChannelID, e.Receipt.Claim, millis(s.now())).Scan(&n)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM receipt_embeddings WHERE channel_id=?`, a.ChannelID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO receipt_embedding_spaces(channel_id,model,version,dimensions) VALUES(?,?,?,?) ON CONFLICT(channel_id) DO UPDATE SET model=excluded.model,version=excluded.version,dimensions=excluded.dimensions`, a.ChannelID, space.Model, space.Version, space.Dimensions); err != nil {
		return err
	}
	for _, e := range entries {
		b, err := json.Marshal(e.Vector)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO receipt_embeddings(receipt_id,channel_id,claim,vector) VALUES(?,?,?,?)`, e.Receipt.ID, a.ChannelID, e.Receipt.Claim, string(b)); err != nil {
			return err
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

// IndexedReceipts fails closed when the configured space changes; reindexing is
// explicit. Only currently eligible receipts belonging to the active owner are
// returned. Invalidation and expiry take effect immediately at query time.
func (s *Store) IndexedReceipts(ctx context.Context, a DetectionAuthority, space retrieval.Space) ([]retrieval.Entry, error) {
	if a.AccountID == "" || a.ChannelID == "" || a.LiveSessionID == "" || a.Generation == 0 || space.Model == "" || space.Version == "" || space.Dimensions <= 0 {
		return nil, ErrInvalid
	}
	var model, version string
	var dim int
	err := s.db.Reader().QueryRowContext(ctx, `SELECT i.model,i.version,i.dimensions FROM receipt_embedding_spaces i JOIN accounts a ON a.channel_id=i.channel_id JOIN live_sessions l ON l.channel_id=i.channel_id WHERE a.id=? AND a.status='connected' AND i.channel_id=? AND l.id=? AND l.state='live' AND l.ended_at IS NULL AND l.runtime_generation=?`, a.AccountID, a.ChannelID, a.LiveSessionID, a.Generation).Scan(&model, &version, &dim)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotEligible
	}
	if err != nil {
		return nil, err
	}
	if model != space.Model || version != space.Version || dim != space.Dimensions {
		return nil, ErrConflict
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT r.id,r.claim,r.version,e.vector FROM receipt_embeddings e JOIN receipts r ON r.id=e.receipt_id AND r.channel_id=e.channel_id WHERE r.channel_id=? AND r.claim=e.claim AND r.validity='valid' AND r.sensitive=0 AND (r.expires_at IS NULL OR r.expires_at>?)`, a.ChannelID, millis(s.now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []retrieval.Entry
	for rows.Next() {
		var id, claim, vector string
		var version int
		if err = rows.Scan(&id, &claim, &version, &vector); err != nil {
			return nil, err
		}
		var v []float64
		if json.Unmarshal([]byte(vector), &v) != nil || !retrieval.ValidVector(v, dim) {
			return nil, ErrConflict
		}
		result = append(result, retrieval.Entry{Receipt: receipt.Receipt{ID: id, ChannelID: a.ChannelID, Claim: claim, Version: version}, Vector: v})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

package store

import (
	"context"
	"receipts/internal/receipt"
)

// LoreReceipts reads the eligible ledger in a single snapshot. Voided,
// sensitive and expired rows are excluded at the database boundary.
func (s *Store) LoreReceipts(ctx context.Context, channel string) ([]receipt.Receipt, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT id,claim,verdict FROM receipts WHERE channel_id=? AND validity='valid' AND sensitive=0 AND (expires_at IS NULL OR expires_at>?) ORDER BY created_at DESC,id DESC`, channel, millis(s.now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []receipt.Receipt
	for rows.Next() {
		var r receipt.Receipt
		if err := rows.Scan(&r.ID, &r.Claim, &r.Verdict); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

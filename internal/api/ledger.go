package api

import (
	"net/http"

	"receipts/internal/auth"
	"receipts/internal/events"
	"receipts/internal/receipt"
	"receipts/internal/store"
)

// LedgerReader is the hosted read model. Middleware supplies the principal
// and supervisor supplies live-session authority; the ledger checks durable
// ownership again. It never returns raw evidence or transcripts to overlays.
type LedgerReader struct {
	Store *store.Store
	Mode  string
}

func (l LedgerReader) session(r *http.Request, a auth.ChannelContext) (receipt.Session, error) {
	principal, err := auth.ChannelFromContext(r.Context())
	if err != nil || l.Store == nil || principal.AccountID != a.AccountID || principal.ChannelID != a.ChannelID || principal.LiveSessionID != a.LiveSessionID || principal.Generation != a.Generation || a.LiveSessionID == "" || a.Generation == 0 {
		return receipt.Session{}, auth.ErrForbidden
	}
	if err := r.Context().Err(); err != nil {
		return receipt.Session{}, err
	}
	return l.Store.OwnedChannelSession(r.Context(), a.AccountID, a.ChannelID, a.LiveSessionID)
}
func (l LedgerReader) OverlaySnapshotFor(r *http.Request, a auth.ChannelContext) (events.OverlaySnapshot, error) {
	session, err := l.session(r, a)
	if err != nil {
		return events.OverlaySnapshot{}, err
	}
	receipts, err := l.Store.PublicReceipts(r.Context(), a.ChannelID, 100)
	if err != nil {
		return events.OverlaySnapshot{}, err
	}
	value := events.OverlaySnapshot{Channel: a.ChannelID, Live: session.State == "live", Receipts: make([]events.PublicReceipt, 0, len(receipts))}
	for _, v := range receipts {
		value.Receipts = append(value.Receipts, events.PublicReceipt{ID: v.ID, Claim: v.Claim, ClipURL: v.ClipURL, ClipStatus: string(v.ClipStatus), Verdict: string(v.Verdict), CreatedAt: v.CreatedAt})
	}
	return value, r.Context().Err()
}
func (l LedgerReader) ReviewQueueFor(r *http.Request, a auth.ChannelContext) ([]events.ReviewItem, error) {
	if a.Device {
		return nil, auth.ErrForbidden
	}
	if _, err := l.session(r, a); err != nil {
		return nil, err
	}
	values, err := l.Store.PendingReviews(r.Context(), a.AccountID, a.ChannelID, 100)
	if err != nil {
		return nil, err
	}
	out := make([]events.ReviewItem, 0, len(values))
	for _, v := range values {
		out = append(out, events.ReviewItem{ID: v.ID, Claim: v.Claim, Reason: "uncertain contradiction", CreatedAt: v.CreatedAt})
	}
	return out, r.Context().Err()
}
func (l LedgerReader) HealthFor(r *http.Request, a auth.ChannelContext) (events.Health, error) {
	if a.Device {
		return events.Health{}, auth.ErrForbidden
	}
	if _, err := l.session(r, a); err != nil {
		return events.Health{}, err
	}
	// P4 supplies fresh provider confirmation. Persisted session state alone
	// must not be presented as authoritative confirmation of an active stream.
	return events.Health{Status: "runtime_ready", Mode: l.Mode, LiveConfirmed: false}, nil
}

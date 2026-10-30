package api

import (
	"errors"
	"net/http"
	"time"

	"receipts/internal/appeal"

	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

var ErrUnimplemented = errors.New("api: application action is not implemented")

// LedgerControls uses the channel from authenticated server context for every
// ledger mutation. IDs in the input are always checked by a channel-scoped
// store operation.
type LedgerControls struct {
	Store          *store.Store
	Runtime        *channelruntime.Manager
	AppealDuration time.Duration
	StampCooldown  time.Duration
}

func (c LedgerControls) SetPausedFor(_ *http.Request, authority auth.ChannelContext, in ControlInput) error {
	if c.Runtime == nil || in.Paused == nil {
		return ErrUnimplemented
	}
	return c.Runtime.SetPaused(authority, *in.Paused)
}
func (c LedgerControls) VoidReceiptFor(r *http.Request, authority auth.ChannelContext, in ControlInput) error {
	return (appeal.Service{Store: c.Store, Runtime: c.Runtime, Duration: c.AppealDuration}).Void(r.Context(), authority, in.ReceiptID)
}
func (c LedgerControls) ResolveReviewFor(r *http.Request, authority auth.ChannelContext, in ControlInput) error {
	return (appeal.ReviewService{Store: c.Store, Runtime: c.Runtime, Cooldown: c.StampCooldown}).Resolve(r.Context(), authority, in.ReviewID, in.ActionKey, in.ReviewDecision == ReviewApprove)
}
func (c LedgerControls) SetReceiptDecisionFor(r *http.Request, authority auth.ChannelContext, in ControlInput) error {
	return (appeal.Service{Store: c.Store, Runtime: c.Runtime, Duration: c.AppealDuration}).Answer(r.Context(), authority, in.ReceiptID, in.ActionKey, string(in.ReceiptDecision), in.Note)
}

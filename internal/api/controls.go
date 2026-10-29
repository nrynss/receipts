package api

import (
	"errors"
	"net/http"

	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

var ErrUnimplemented = errors.New("api: application action is not implemented")

// LedgerControls uses the channel from authenticated server context for every
// ledger mutation. IDs in the input are always checked by a channel-scoped
// store operation.
type LedgerControls struct {
	Store   *store.Store
	Runtime *channelruntime.Manager
}

func (c LedgerControls) SetPausedFor(_ *http.Request, authority auth.ChannelContext, in ControlInput) error {
	if c.Runtime == nil || in.Paused == nil {
		return ErrUnimplemented
	}
	return c.Runtime.SetPaused(authority, *in.Paused)
}
func (c LedgerControls) VoidReceiptFor(r *http.Request, authority auth.ChannelContext, in ControlInput) error {
	return ErrUnimplemented
}
func (c LedgerControls) ResolveReviewFor(r *http.Request, authority auth.ChannelContext, in ControlInput) error {
	return ErrUnimplemented
}
func (c LedgerControls) SetReceiptDecisionFor(_ *http.Request, _ auth.ChannelContext, _ ControlInput) error {
	return ErrUnimplemented
}

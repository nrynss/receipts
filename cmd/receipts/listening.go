package main

import (
	"context"
	"errors"
	"receipts/internal/api"
	"receipts/internal/auth"
	"receipts/internal/twitch/connection"
)

// Observations help setup without granting live capture authority.
type listeningObservation struct{ adapter *connection.Adapter }

func (l listeningObservation) CanStart() bool { return false }
func (l listeningObservation) Start(context.Context, auth.ChannelContext) error {
	return errors.New("trusted live activation unavailable")
}
func (l listeningObservation) Status(ctx context.Context, a auth.ChannelContext) (api.ListeningState, error) {
	status, err := l.adapter.ProbeStreamStatus(ctx, a)
	if errors.Is(err, auth.ErrReconnect) || errors.Is(err, auth.ErrUnauthorized) || errors.Is(err, connection.ErrLost) {
		return api.StateMissingGrant, nil
	}
	if err != nil {
		return api.StateUnknown, err
	}
	if !status.Known {
		return api.StateUnknown, nil
	}
	if !status.Live {
		return api.StateOffline, nil
	}
	return api.StateNotListening, nil
}

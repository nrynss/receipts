package api

import (
	"context"
	"encoding/json"
	"net/http"

	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
)

// AccountHandler remains available while the owner's channel is offline.
func AccountHandler(sessions *auth.Service, supervisor *channelruntime.Manager, disconnect func(context.Context, string) error, budgets *channelruntime.Budgets) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /account-api/status", func(w http.ResponseWriter, r *http.Request) {
		identity, err := auth.ChannelFromContext(r.Context())
		if err != nil || identity.Device {
			http.Error(w, "Sign in required", 401)
			return
		}
		ctx, channel, cancel, err := supervisor.BindRequest(r.Context(), identity)
		_ = ctx
		state := "not_listening"
		paused := false
		if err == nil {
			defer cancel()
			state = "listening"
			paused = channel.Paused()
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"state": state, "paused": paused})
	})
	mux.HandleFunc("POST /account-api/stop", func(w http.ResponseWriter, r *http.Request) {
		identity, err := auth.ChannelFromContext(r.Context())
		if err != nil || identity.Device {
			http.Error(w, "Sign in required", 401)
			return
		}
		_, channel, cancel, err := supervisor.BindRequest(r.Context(), identity)
		if err != nil {
			http.Error(w, "Not listening", 409)
			return
		}
		defer cancel()
		if err = supervisor.Stop(channel.Authority); err != nil {
			http.Error(w, "Could not stop", 409)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /account-api/disconnect", func(w http.ResponseWriter, r *http.Request) {
		identity, err := auth.ChannelFromContext(r.Context())
		if err != nil || identity.Device {
			http.Error(w, "Sign in required", http.StatusUnauthorized)
			return
		}
		if disconnect == nil {
			http.Error(w, "Disconnect unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := disconnect(r.Context(), identity.AccountID); err != nil {
			http.Error(w, "Disconnect could not be confirmed", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /account-api/delete", func(w http.ResponseWriter, r *http.Request) {
		identity, err := auth.ChannelFromContext(r.Context())
		if err != nil || identity.Device {
			http.Error(w, "Sign in required", http.StatusUnauthorized)
			return
		}
		if disconnect == nil || budgets == nil {
			http.Error(w, "Deletion unavailable", http.StatusServiceUnavailable)
			return
		}
		supervisor.StopAccount(identity.AccountID)
		disconnectErr := disconnect(r.Context(), identity.AccountID)
		supervisor.StopAccount(identity.AccountID)
		if err := sessions.EraseDisconnectedAccount(r.Context(), identity.AccountID, budgets.ForgetChannel); err != nil {
			http.Error(w, "Deletion could not be completed", http.StatusConflict)
			return
		}
		if disconnectErr != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted_provider_revoke_unconfirmed"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return sessions.ChannelMiddleware(mux)
}

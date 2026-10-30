package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
)

// ListeningState is an owner-facing observation, never capture authority.
type ListeningState string

const (
	StateUnavailable   ListeningState = "unavailable"
	StateUnknown       ListeningState = "unknown"
	StateOffline       ListeningState = "offline"
	StateMissingGrant  ListeningState = "missing_grant"
	StateCapacity      ListeningState = "capacity"
	StateNotListening  ListeningState = "not_listening"
	StateListening     ListeningState = "listening"
	StateSessionActive ListeningState = "session_active"
	StatePaused        ListeningState = "paused"
)

// ListeningBackend provides read-only observations and explicit activation.
// Start must use the trusted live-session activation path, not the observation.
type ListeningBackend interface {
	Status(context.Context, auth.ChannelContext) (ListeningState, error)
	Start(context.Context, auth.ChannelContext) error
	CanStart() bool
}

type SessionMicBackend interface {
	StartWithMicrophone(context.Context, auth.ChannelContext, bool) error
}

type SessionOptionsBackend interface {
	StartWithOptions(context.Context, auth.ChannelContext, bool, bool) error
}

type AudioReadiness interface{ AudioCaptureActive() bool }
type ScopedAudioReadiness interface {
	AudioCaptureFor(auth.ChannelContext) bool
}
type ListeningStopper interface {
	Stop(context.Context, auth.ChannelContext) error
}

// AccountHandler remains available while the owner's channel is offline.
func AccountHandler(sessions *auth.Service, supervisor *channelruntime.Manager, disconnect func(context.Context, string) error, budgets *channelruntime.Budgets) http.Handler {
	return AccountHandlerWithBackend(sessions, supervisor, disconnect, budgets, nil)
}

func AccountHandlerWithBackend(sessions *auth.Service, supervisor *channelruntime.Manager, disconnect func(context.Context, string) error, budgets *channelruntime.Budgets, backend ListeningBackend) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /account-api/status", func(w http.ResponseWriter, r *http.Request) {
		identity, err := auth.ChannelFromContext(r.Context())
		if err != nil || identity.Device {
			http.Error(w, "Sign in required", 401)
			return
		}
		_, channel, cancel, err := supervisor.BindRequest(r.Context(), identity)
		state := StateUnavailable
		if err == nil {
			defer cancel()
			state = StateSessionActive
			if backend != nil {
				observed, statusErr := backend.Status(r.Context(), identity)
				if statusErr != nil {
					state = StateUnknown
				} else {
					switch observed {
					case StateOffline, StateUnknown, StateMissingGrant, StateCapacity, StateNotListening, StateUnavailable, StateSessionActive:
						state = observed
					default:
						state = StateUnknown
					}
				}
			}
			_ = channel
		} else if backend != nil {
			observed, statusErr := backend.Status(r.Context(), identity)
			if statusErr != nil {
				state = StateUnknown
			} else {
				switch observed {
				case StateOffline, StateUnknown, StateMissingGrant, StateCapacity, StateNotListening, StateUnavailable, StateSessionActive:
					state = observed
				default:
					state = StateUnknown
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		audioActive := false
		if readiness, ok := backend.(ScopedAudioReadiness); ok {
			audioActive = readiness.AudioCaptureFor(identity)
		} else if readiness, ok := backend.(AudioReadiness); ok {
			audioActive = readiness.AudioCaptureActive()
		}
		if state == StateSessionActive && audioActive {
			state = StateListening
			if _, channel, release, err := supervisor.BindRequest(r.Context(), identity); err == nil {
				if channel.Paused() {
					state = StatePaused
				}
				release()
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"state": state, "can_start": backend != nil && backend.CanStart(), "audio_capture_active": audioActive})
	})
	mux.HandleFunc("POST /account-api/start", func(w http.ResponseWriter, r *http.Request) {
		identity, err := auth.ChannelFromContext(r.Context())
		if err != nil || identity.Device {
			http.Error(w, "Sign in required", http.StatusUnauthorized)
			return
		}
		if backend == nil || !backend.CanStart() {
			http.Error(w, "Listening unavailable", http.StatusServiceUnavailable)
			return
		}
		var request struct {
			MicrophonePermission bool `json:"microphone_permission"`
			SoleBroadcaster      bool `json:"sole_broadcaster"`
		}
		if r.Body != nil {
			decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
			if decodeErr := decoder.Decode(&request); decodeErr != nil && decodeErr != io.EOF {
				http.Error(w, "Invalid start request", http.StatusBadRequest)
				return
			}
		}
		if options, ok := backend.(SessionOptionsBackend); ok {
			err = options.StartWithOptions(r.Context(), identity, request.MicrophonePermission, request.SoleBroadcaster)
		} else if mic, ok := backend.(SessionMicBackend); ok {
			err = mic.StartWithMicrophone(r.Context(), identity, request.MicrophonePermission)
		} else {
			err = backend.Start(r.Context(), identity)
		}
		if err != nil {
			http.Error(w, "Listening could not start", http.StatusServiceUnavailable)
			return
		}
		_, _, cancel, err := supervisor.BindRequest(r.Context(), identity)
		if err != nil {
			http.Error(w, "Listening unavailable", http.StatusServiceUnavailable)
			return
		}
		defer cancel()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /account-api/stop", func(w http.ResponseWriter, r *http.Request) {
		identity, err := auth.ChannelFromContext(r.Context())
		if err != nil || identity.Device {
			http.Error(w, "Sign in required", 401)
			return
		}
		if stopper, ok := backend.(ListeningStopper); ok {
			if err := stopper.Stop(r.Context(), identity); err != nil {
				http.Error(w, "Could not stop", http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusNoContent)
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

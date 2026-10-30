// Package api exposes authenticated overlay and streamer HTTP contracts.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nrynss/keel/wire"
	"receipts/internal/auth"
	"receipts/internal/events"
	channelruntime "receipts/internal/runtime"
)

type Reader interface {
	OverlaySnapshot(r *http.Request) (events.OverlaySnapshot, error)
	ReviewQueue(r *http.Request) ([]events.ReviewItem, error)
	Health(r *http.Request) (events.Health, error)
}

// Controls are deliberately explicit. Implementations own authorization-free
// application operations; this package enforces the control credential.
type Controls interface {
	SetPaused(r *http.Request, input ControlInput) error
	VoidReceipt(r *http.Request, input ControlInput) error
	ResolveReview(r *http.Request, input ControlInput) error
	SetReceiptDecision(r *http.Request, input ControlInput) error
}

// ScopedReader and ScopedControls are the only application seams accepted by
// hosted mode. Every lookup receives server-validated owner/channel authority.
type ScopedReader interface {
	OverlaySnapshotFor(*http.Request, auth.ChannelContext) (events.OverlaySnapshot, error)
	ReviewQueueFor(*http.Request, auth.ChannelContext) ([]events.ReviewItem, error)
	HealthFor(*http.Request, auth.ChannelContext) (events.Health, error)
}
type ScopedControls interface {
	SetPausedFor(*http.Request, auth.ChannelContext, ControlInput) error
	VoidReceiptFor(*http.Request, auth.ChannelContext, ControlInput) error
	ResolveReviewFor(*http.Request, auth.ChannelContext, ControlInput) error
	SetReceiptDecisionFor(*http.Request, auth.ChannelContext, ControlInput) error
}
type ReviewDecision string

const (
	ReviewApprove ReviewDecision = "approve"
	ReviewReject  ReviewDecision = "reject"
)

type ReceiptDecision string

const (
	DecisionAdmit          ReceiptDecision = "admit"
	DecisionAppeal         ReceiptDecision = "appeal"
	DecisionContextChanged ReceiptDecision = "context_changed"
)

type ControlInput struct {
	ActionKey       string          `json:"action_key,omitempty"`
	ReceiptID       string          `json:"receipt_id,omitempty"`
	ReviewID        string          `json:"review_id,omitempty"`
	Paused          *bool           `json:"paused,omitempty"`
	Note            string          `json:"note,omitempty"`
	ReviewDecision  ReviewDecision  `json:"decision,omitempty"`
	ReceiptDecision ReceiptDecision `json:"receipt_decision,omitempty"`
}
type Config struct {
	OverlayCredential, ControlCredential string
	Reader                               Reader
	Controls                             Controls
	Events                               *events.Publisher
	EventsFor                            func(auth.ChannelContext) *events.Publisher
	OwnerReader                          ScopedReader
	OwnerControls                        ScopedControls
}
type Server struct {
	cfg            Config
	mux            *http.ServeMux
	requireChannel bool
	ownerRuntime   *channelruntime.Manager
}

func New(cfg Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /api/overlay", s.overlay)
	s.mux.HandleFunc("GET /api/events", s.overlayEvents)
	s.mux.HandleFunc("GET /api/review", s.review)
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("POST /api/control/pause", s.control("pause"))
	s.mux.HandleFunc("POST /api/control/void", s.control("void"))
	s.mux.HandleFunc("POST /api/control/review", s.control("review"))
	s.mux.HandleFunc("POST /api/control/decision", s.control("decision"))
	return s
}

// NewForOwner creates the hosted account API. Every request receives owner
// and channel authority from TA.1's opaque session; mutations also pass CSRF
// and origin validation in the auth middleware.
func NewForOwner(cfg Config, sessions *auth.Service, supervisor *channelruntime.Manager) http.Handler {
	server := New(cfg)
	server.requireChannel = true
	server.ownerRuntime = supervisor
	if sessions == nil || supervisor == nil {
		return unavailableHandler()
	}
	return sessions.ChannelMiddleware(supervisor.Middleware(server))
}

func unavailableHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { unavailable(w) })
}

func (s *Server) channelAuthorized(r *http.Request) bool {
	if !s.requireChannel {
		return true
	}
	_, err := auth.ChannelFromContext(r.Context())
	if err != nil {
		return false
	}
	identity, _ := auth.ChannelFromContext(r.Context())
	return identity.LiveSessionID != "" && identity.Generation != 0
}

func (s *Server) publisher(r *http.Request) *events.Publisher {
	if s.requireChannel {
		channel, ok := channelruntime.ChannelFromContext(r.Context())
		if !ok || channel == nil {
			return nil
		}
		return channel.Events
	}
	return s.cfg.Events
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) validCredentials() bool {
	return s.cfg.OverlayCredential != "" && s.cfg.ControlCredential != "" && s.cfg.OverlayCredential != s.cfg.ControlCredential
}

func equalCredential(want, got string) bool {
	return want != "" && len(want) == len(got) && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}
func (s *Server) overlayAuth(r *http.Request) bool {
	if s.requireChannel {
		_, err := auth.ChannelFromContext(r.Context())
		return err == nil
	}
	if !s.validCredentials() {
		return false
	}
	got := r.Header.Get("Authorization")
	if !strings.HasPrefix(got, "Bearer ") {
		return false
	}
	got = strings.TrimPrefix(got, "Bearer ")
	return equalCredential(s.cfg.OverlayCredential, got)
}
func (s *Server) controlAuth(r *http.Request) bool {
	if s.requireChannel {
		value, err := auth.ChannelFromContext(r.Context())
		return err == nil && !value.Device
	}
	if !s.validCredentials() {
		return false
	}
	got := r.Header.Get("Authorization")
	if !strings.HasPrefix(got, "Bearer ") {
		return false
	}
	return equalCredential(s.cfg.ControlCredential, strings.TrimPrefix(got, "Bearer "))
}
func apiError(w http.ResponseWriter, status int, code, message string) {
	_ = wire.WriteError(w, status, code, message, nil)
}
func unauthorized(w http.ResponseWriter) {
	apiError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required")
}
func unavailable(w http.ResponseWriter) {
	apiError(w, http.StatusServiceUnavailable, "handler_unavailable", "The requested handler is unavailable")
}
func encode(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func (s *Server) overlay(w http.ResponseWriter, r *http.Request) {
	if !s.channelAuthorized(r) {
		unauthorized(w)
		return
	}
	if !s.overlayAuth(r) {
		unauthorized(w)
		return
	}
	if s.requireChannel {
		if s.cfg.OwnerReader == nil {
			unavailable(w)
			return
		}
		identity, _ := auth.ChannelFromContext(r.Context())
		channel, ok := channelruntime.ChannelFromContext(r.Context())
		if !ok || channel == nil {
			unavailable(w)
			return
		}
		value, err := channel.Events.Snapshot(func() (events.OverlaySnapshot, error) { return s.cfg.OwnerReader.OverlaySnapshotFor(r, identity) })
		if err != nil {
			apiError(w, http.StatusInternalServerError, "snapshot_unavailable", "The overlay snapshot is unavailable")
			return
		}
		encode(w, value)
		return
	}
	if s.cfg.Reader == nil {
		unavailable(w)
		return
	}
	var value events.OverlaySnapshot
	var err error
	publisher := s.publisher(r)
	if publisher != nil {
		value, err = publisher.Snapshot(func() (events.OverlaySnapshot, error) { return s.cfg.Reader.OverlaySnapshot(r) })
	} else {
		if s.requireChannel {
			unavailable(w)
			return
		}
		value, err = s.cfg.Reader.OverlaySnapshot(r)
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "snapshot_unavailable", "The overlay snapshot is unavailable")
		return
	}
	encode(w, value)
}
func (s *Server) overlayEvents(w http.ResponseWriter, r *http.Request) {
	if !s.channelAuthorized(r) {
		unauthorized(w)
		return
	}
	if !s.overlayAuth(r) {
		unauthorized(w)
		return
	}
	publisher := s.publisher(r)
	if publisher == nil {
		unavailable(w)
		return
	}
	// Keel performs bounded Last-Event-ID replay. Clients must open the stream,
	// fetch /api/overlay, discard replayed sequence <= snapshot.Sequence, then
	// apply only contiguous events from the snapshot epoch. Explicit id: 0
	// resets Chaaya's numeric cursor after server restart, when Keel's new
	// process-local broker starts IDs over. Chaaya's event filter must include
	// "reset" so it observes this cursor control frame.
	publisher.Broker.ServeTopic(&resetFrameWriter{ResponseWriter: w}, r, events.TopicOverlay)
}
func (s *Server) review(w http.ResponseWriter, r *http.Request) {
	if !s.channelAuthorized(r) {
		unauthorized(w)
		return
	}
	if !s.controlAuth(r) {
		unauthorized(w)
		return
	}
	if s.requireChannel {
		identity, _ := auth.ChannelFromContext(r.Context())
		if identity.Device {
			unauthorized(w)
			return
		}
		if s.cfg.OwnerReader == nil {
			unavailable(w)
			return
		}
		value, err := s.cfg.OwnerReader.ReviewQueueFor(r, identity)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "review_queue_unavailable", "The review queue is unavailable")
			return
		}
		encode(w, value)
		return
	}
	if s.cfg.Reader == nil {
		unavailable(w)
		return
	}
	value, err := s.cfg.Reader.ReviewQueue(r)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "review_queue_unavailable", "The review queue is unavailable")
		return
	}
	encode(w, value)
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if !s.channelAuthorized(r) {
		unauthorized(w)
		return
	}
	if !s.controlAuth(r) {
		unauthorized(w)
		return
	}
	if s.requireChannel {
		identity, _ := auth.ChannelFromContext(r.Context())
		if identity.Device {
			unauthorized(w)
			return
		}
		if s.cfg.OwnerReader == nil {
			unavailable(w)
			return
		}
		value, err := s.cfg.OwnerReader.HealthFor(r, identity)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "health_unavailable", "Health information is unavailable")
			return
		}
		encode(w, value)
		return
	}
	if s.cfg.Reader == nil {
		unavailable(w)
		return
	}
	value, err := s.cfg.Reader.Health(r)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "health_unavailable", "Health information is unavailable")
		return
	}
	encode(w, value)
}
func (s *Server) control(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.channelAuthorized(r) {
			unauthorized(w)
			return
		}
		if !s.controlAuth(r) {
			unauthorized(w)
			return
		}
		if s.cfg.Controls == nil {
			if s.requireChannel && s.cfg.OwnerControls == nil {
				apiError(w, http.StatusNotImplemented, "not_implemented", "This control action is not implemented")
				return
			}
		}
		if s.requireChannel {
			identity, _ := auth.ChannelFromContext(r.Context())
			if identity.Device {
				unauthorized(w)
				return
			}
		}
		if !s.requireChannel && s.cfg.Controls == nil {
			apiError(w, http.StatusNotImplemented, "not_implemented", "This control action is not implemented")
			return
		}
		var input ControlInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_request", "The control request is invalid")
			return
		}
		var err error
		switch action {
		case "pause":
			if input.Paused == nil {
				apiError(w, http.StatusBadRequest, "invalid_request", "paused is required")
				return
			}
			if s.requireChannel {
				identity, _ := auth.ChannelFromContext(r.Context())
				err = s.cfg.OwnerControls.SetPausedFor(r, identity, input)
			} else {
				err = s.cfg.Controls.SetPaused(r, input)
			}
		case "void":
			if input.ReceiptID == "" {
				apiError(w, http.StatusBadRequest, "invalid_request", "receipt_id is required")
				return
			}
			if s.requireChannel {
				identity, _ := auth.ChannelFromContext(r.Context())
				err = s.cfg.OwnerControls.VoidReceiptFor(r, identity, input)
			} else {
				err = s.cfg.Controls.VoidReceipt(r, input)
			}
		case "review":
			if s.requireChannel && input.ActionKey == "" {
				apiError(w, http.StatusBadRequest, "invalid_request", "action_key is required")
				return
			}
			if input.ReviewID == "" || (input.ReviewDecision != ReviewApprove && input.ReviewDecision != ReviewReject) {
				apiError(w, http.StatusBadRequest, "invalid_request", "review_id and an approve or reject decision are required")
				return
			}
			if s.requireChannel {
				identity, _ := auth.ChannelFromContext(r.Context())
				err = s.cfg.OwnerControls.ResolveReviewFor(r, identity, input)
			} else {
				err = s.cfg.Controls.ResolveReview(r, input)
			}
		case "decision":
			if s.requireChannel && input.ActionKey == "" {
				apiError(w, http.StatusBadRequest, "invalid_request", "action_key is required")
				return
			}
			if input.ReceiptID == "" || (input.ReceiptDecision != DecisionAdmit && input.ReceiptDecision != DecisionAppeal && input.ReceiptDecision != DecisionContextChanged) {
				apiError(w, http.StatusBadRequest, "invalid_request", "receipt_id and a valid receipt_decision are required")
				return
			}
			if s.requireChannel {
				identity, _ := auth.ChannelFromContext(r.Context())
				err = s.cfg.OwnerControls.SetReceiptDecisionFor(r, identity, input)
			} else {
				err = s.cfg.Controls.SetReceiptDecision(r, input)
			}
		}
		if err != nil {
			if errors.Is(err, ErrUnimplemented) {
				apiError(w, http.StatusNotImplemented, "not_implemented", "This control action is not implemented")
				return
			}
			apiError(w, http.StatusConflict, "control_failed", "The control action could not be applied")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// resetFrameWriter injects the Chaaya cursor-reset frame only after Keel's
// ServeTopic has atomically registered the subscription/replay snapshot.
type resetFrameWriter struct {
	http.ResponseWriter
	sent bool
}

func (w *resetFrameWriter) Write(p []byte) (int, error) {
	if !w.sent {
		w.sent = true
		if err := wire.WriteEvent(w.ResponseWriter, "reset", 0, struct{}{}); err != nil {
			return 0, err
		}
	}
	return w.ResponseWriter.Write(p)
}
func (w *resetFrameWriter) FlushError() error {
	if !w.sent {
		w.sent = true
		if err := wire.WriteEvent(w.ResponseWriter, "reset", 0, struct{}{}); err != nil {
			return err
		}
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

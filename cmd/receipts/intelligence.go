package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"receipts/internal/auth"
	"receipts/internal/claims"
	"receipts/internal/intelligence"
	"receipts/internal/retrieval"
	runtime "receipts/internal/runtime"
	"receipts/internal/store"
	"receipts/internal/twitch/chat"
	"receipts/internal/twitch/connection"
	"receipts/internal/twitch/eventsub"
)

type intelligenceAttachment struct {
	ledger                                                    *store.Store
	runtime                                                   *runtime.Manager
	adapter                                                   *connection.Adapter
	apiKey, extractionModel, embeddingModel, embeddingVersion string
	embeddingDimensions                                       int
	services                                                  sync.Map
}

// Non-receipt commands are not serviced by this attachment. A rejected or
// failed capture does not tear down the subscribed EventSub connection.
type captureSink struct{ router *chat.Router }

func (s captureSink) HandleEvent(ctx context.Context, raw json.RawMessage, at time.Time) error {
	_ = s.router.HandleEvent(ctx, raw, at)
	return nil
}
func (a *intelligenceAttachment) attach(ctx context.Context, authority auth.ChannelContext) (func(), error) {
	if a == nil || a.ledger == nil || a.runtime == nil || a.adapter == nil || a.adapter.Resolver == nil {
		return nil, errors.New("intelligence: attachment unavailable")
	}
	grant, err := a.adapter.Resolver.Resolve(ctx, authority)
	if err != nil {
		return nil, err
	}
	if grant.SubjectID == "" || grant.SubjectID != grant.ChannelPlatformID {
		return nil, connection.ErrBinding
	}
	model := a.extractionModel
	if model == "" {
		model = claims.DefaultModel
	}
	embedding := a.embeddingModel
	if embedding == "" {
		embedding = retrieval.DefaultModel
	}
	if model != claims.DefaultModel || embedding != retrieval.DefaultModel {
		return nil, errors.New("intelligence: unsupported metered model")
	}
	extractor := claims.Client{APIKey: a.apiKey, Model: model}
	space := retrieval.Space{Model: embedding, Version: a.embeddingVersion, Dimensions: a.embeddingDimensions}
	svc := &intelligence.Service{Authority: authority, Meter: a.runtime, Ledger: a.ledger, Extractor: extractor, Sensitive: extractor, Embedder: retrieval.Client{APIKey: a.apiKey, Space: space}, Index: a.ledger, Space: space, ExtractionModel: model}
	a.services.Store(authority.ChannelID, svc)
	router := &chat.Router{Authority: authority, ChannelPlatformID: grant.ChannelPlatformID, Capture: svc}
	client := &eventsub.Client{Helix: eventsub.Adapter{Connection: a.adapter}, Sink: captureSink{router}, OnAuthorizationLost: func(error) { _ = a.runtime.Cancel(authority) }}
	run, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); _ = client.Run(run, authority) }()
	return func() { cancel(); <-done; a.services.CompareAndDelete(authority.ChannelID, svc) }, nil
}

// reindex requires the existing owner session, CSRF/origin validation and
// runtime binding supplied by the middleware at registration.
func (a *intelligenceAttachment) reindex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	identity, err := auth.ChannelFromContext(r.Context())
	if err != nil || identity.Device {
		http.Error(w, "owner required", http.StatusForbidden)
		return
	}
	value, ok := a.services.Load(identity.ChannelID)
	if !ok {
		http.Error(w, "index unavailable", http.StatusServiceUnavailable)
		return
	}
	svc := value.(*intelligence.Service)
	if svc.Authority.AccountID != identity.AccountID || svc.Authority.LiveSessionID != identity.LiveSessionID || svc.Authority.Generation != identity.Generation {
		http.Error(w, "index unavailable", http.StatusServiceUnavailable)
		return
	}
	if err = svc.Reindex(r.Context()); err != nil {
		http.Error(w, "reindex failed", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

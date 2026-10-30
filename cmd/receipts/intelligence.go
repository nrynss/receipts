package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"receipts/internal/appeal"
	"receipts/internal/auth"
	"receipts/internal/claims"
	"receipts/internal/intelligence"
	"receipts/internal/judge"
	"receipts/internal/lore"
	"receipts/internal/receipt"
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
	judgeModel                                                string
	embeddingDimensions                                       int
	services                                                  sync.Map
	judgeQueues                                               sync.Map
}

// One worker serializes receipt processing without blocking the EventSub reader.
// Full queues drop new events: backpressure never creates unbounded work.
const captureQueueSize = 32

type eventHandler interface {
	HandleEvent(context.Context, json.RawMessage, time.Time) error
}
type queuedEvent struct {
	raw json.RawMessage
	at  time.Time
}
type captureSink struct {
	ctx     context.Context
	cancel  context.CancelFunc
	events  chan queuedEvent
	done    chan struct{}
	handler eventHandler
}

func newCaptureSink(parent context.Context, handler eventHandler, capacity int) *captureSink {
	ctx, cancel := context.WithCancel(parent)
	s := &captureSink{ctx: ctx, cancel: cancel, events: make(chan queuedEvent, capacity), done: make(chan struct{}), handler: handler}
	go s.run()
	return s
}
func (s *captureSink) run() {
	defer close(s.done)
	for {
		select {
		case <-s.ctx.Done():
			return
		case event := <-s.events:
			// Cancellation wins even when the queue is already ready.
			if s.ctx.Err() != nil {
				return
			}
			_ = s.handler.HandleEvent(s.ctx, event.raw, event.at)
		}
	}
}
func (s *captureSink) HandleEvent(ctx context.Context, raw json.RawMessage, at time.Time) error {
	if ctx.Err() != nil || s.ctx.Err() != nil || len(raw) == 0 || len(raw) > chat.MaxMessageBytes*4 {
		return nil
	}
	var message struct {
		Message struct {
			Text string `json:"text"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &message) != nil {
		return nil
	}
	words := strings.Fields(message.Message.Text)
	if len(words) == 0 || !strings.EqualFold(words[0], "!receipt") {
		return nil
	}
	event := queuedEvent{raw: append(json.RawMessage(nil), raw...), at: at}
	select {
	case <-ctx.Done():
	case <-s.ctx.Done():
	case s.events <- event:
	default:
	}
	return nil
}
func (s *captureSink) stop() { s.cancel(); <-s.done }

func (a *intelligenceAttachment) attach(ctx context.Context, authority auth.ChannelContext) (func(), error) {
	if a == nil || a.ledger == nil || a.runtime == nil || a.adapter == nil || a.adapter.Resolver == nil {
		return nil, errors.New("intelligence: attachment unavailable")
	}
	svc, err := a.buildIntelligenceService(authority)
	if err != nil {
		return nil, err
	}
	grant, err := a.adapter.Resolver.Resolve(ctx, authority)
	if err != nil {
		return nil, err
	}
	if grant.SubjectID == "" || grant.SubjectID != grant.ChannelPlatformID {
		return nil, connection.ErrBinding
	}
	run, cancel := context.WithCancel(ctx)
	a.services.Store(authority.ChannelID, svc)
	queue := newJudgeQueue(run, authority, 16, svc.JudgeSource)
	a.judgeQueues.Store(authority.ChannelID, queue)
	router := &chat.Router{
		Authority: authority, ChannelPlatformID: grant.ChannelPlatformID,
		Capture: svc, Appeals: appeal.Service{Store: a.ledger, Runtime: a.runtime},
		Lore: lore.Service{Ledger: a.ledger}, Sender: a.adapter,
	}
	sink := newCaptureSink(run, router, captureQueueSize)
	client := &eventsub.Client{Helix: eventsub.Adapter{Connection: a.adapter}, Sink: sink, OnAuthorizationLost: func(error) { _ = a.runtime.Cancel(authority) }}
	done := make(chan struct{})
	go func() { defer close(done); _ = client.Run(run, authority) }()
	return func() {
		cancel()
		<-done
		sink.stop()
		a.judgeQueues.CompareAndDelete(authority.ChannelID, queue)
		queue.wait()
		a.services.CompareAndDelete(authority.ChannelID, svc)
	}, nil
}

func (a *intelligenceAttachment) buildIntelligenceService(authority auth.ChannelContext) (*intelligence.Service, error) {
	if a == nil {
		return nil, errors.New("intelligence: attachment unavailable")
	}
	model := a.extractionModel
	if model == "" {
		model = claims.LunaModel
	}
	if model != claims.LunaModel {
		return nil, errors.New("RECEIPTS_EXTRACTION_MODEL must be gpt-6-luna; gpt-4o-mini-2024-07-18 is reserved for historical evaluation only")
	}
	judgeModel := a.judgeModel
	if judgeModel == "" {
		judgeModel = judge.DecisionsModel
	}
	if judgeModel != judge.DecisionsModel {
		return nil, errors.New("RECEIPTS_JUDGE_MODEL must be gpt-6-luna for Decisions classification; gpt-4o-mini-2024-07-18 is reserved for historical evaluation only")
	}
	embedding := a.embeddingModel
	if embedding == "" {
		embedding = retrieval.DefaultModel
	}
	if embedding != retrieval.DefaultModel {
		return nil, errors.New("RECEIPTS_EMBEDDING_MODEL must be text-embedding-3-small")
	}
	space := retrieval.Space{Model: embedding, Version: a.embeddingVersion, Dimensions: a.embeddingDimensions}
	extractor := claims.Client{APIKey: a.apiKey, Model: model}
	return &intelligence.Service{
		Authority: authority, Meter: a.runtime, Ledger: a.ledger,
		Extractor: extractor, Sensitive: claims.DecisionsClient{APIKey: a.apiKey},
		Embedder: retrieval.Client{APIKey: a.apiKey, Space: space},
		Index:    a.ledger, Space: space, ExtractionModel: model,
		Judge: judge.DecisionsClient{APIKey: a.apiKey}, DetectionStore: a.ledger,
	}, nil
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

// judgeQueue serializes paid work; overload drops new segments and cancellation
// discards queued work. The authority is captured with the queue generation.
type judgeQueue struct {
	ctx       context.Context
	authority auth.ChannelContext
	segments  chan receipt.Segment
	done      chan struct{}
}

func newJudgeQueue(ctx context.Context, authority auth.ChannelContext, capacity int, handle func(context.Context, receipt.Segment) error) *judgeQueue {
	q := &judgeQueue{ctx: ctx, authority: authority, segments: make(chan receipt.Segment, capacity), done: make(chan struct{})}
	go func() {
		defer close(q.done)
		for {
			select {
			case <-ctx.Done():
				return
			case seg := <-q.segments:
				if ctx.Err() != nil {
					return
				}
				_ = handle(ctx, seg)
			}
		}
	}()
	return q
}
func (q *judgeQueue) enqueue(authority auth.ChannelContext, seg receipt.Segment) {
	if authority != q.authority || q.ctx.Err() != nil {
		return
	}
	select {
	case <-q.ctx.Done():
	case q.segments <- seg:
	default:
	}
}
func (q *judgeQueue) wait() { <-q.done }
func (a *intelligenceAttachment) enqueue(authority auth.ChannelContext, seg receipt.Segment) {
	v, ok := a.judgeQueues.Load(authority.ChannelID)
	if !ok {
		return
	}
	v.(*judgeQueue).enqueue(authority, seg)
}

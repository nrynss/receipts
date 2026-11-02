// Package intelligence ties claim and embedding calls to a live runtime.
package intelligence

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/id"
	"receipts/internal/auth"
	"receipts/internal/commands"
	"receipts/internal/judge"
	"receipts/internal/ports"
	"receipts/internal/providerusage"
	"receipts/internal/receipt"
	"receipts/internal/retrieval"
	runtime "receipts/internal/runtime"
	"receipts/internal/store"
)

// Reservations cover the bounded Luna extraction and Decisions input sizes at
// uncached standard-tier rates with headroom. Unmeasured attempts settle the
// reservation; measured usage reconciles against it.
const extractionEstimate cost.Price = 25_000_000
const classificationEstimate cost.Price = 2_000_000
const embeddingEstimate cost.Price = 1_000_000

type Meter interface {
	Resolve(auth.ChannelContext) (*runtime.Channel, error)
	WithActive(auth.ChannelContext, func(*runtime.Channel) error) error
	Spend(context.Context, auth.ChannelContext, string, string, cost.Price, cost.Work) error
}
type Classifier interface {
	Sensitive(context.Context, string) (bool, error)
}
type Embedder interface {
	Embed(context.Context, string) ([]float64, error)
}
type Index interface {
	EmbeddingSpace(context.Context, store.DetectionAuthority) (retrieval.Space, error)
	EmbeddingCandidates(context.Context, store.DetectionAuthority) ([]receipt.Receipt, error)
	ReindexEmbeddings(context.Context, store.DetectionAuthority, retrieval.Space, []retrieval.Entry) error
	IndexedReceipts(context.Context, store.DetectionAuthority, retrieval.Space) ([]retrieval.Entry, error)
	ValidateDetectionSource(context.Context, store.DetectionAuthority, receipt.Segment) error
}

type Judge interface {
	Judge(context.Context, receipt.Receipt, receipt.Evidence, receipt.Segment) (judge.Result, error)
}
type Service struct {
	OnCandidates    func([]retrieval.Match)
	Now             func() time.Time
	Authority       auth.ChannelContext
	Judge           Judge
	DetectionStore  *store.Store
	Meter           Meter
	Ledger          commands.Ledger
	Extractor       commands.Extractor
	Sensitive       Classifier
	Embedder        Embedder
	Index           Index
	Space           retrieval.Space
	ExtractionModel string
	indexMu         sync.Mutex
}

func (s *Service) validate() error {
	if s == nil || s.Meter == nil || s.Authority.Device || s.Authority.AccountID == "" || s.Authority.ChannelID == "" || s.Authority.LiveSessionID == "" || s.Authority.Generation == 0 {
		return errors.New("intelligence: owner runtime required")
	}
	// The old model remains accepted for offline historical service replay.
	// The production attachment separately hard-requires Luna.
	if (s.ExtractionModel != "gpt-6-luna" && s.ExtractionModel != "gpt-4o-mini-2024-07-18") || s.Space.Model != "text-embedding-3-small" || s.Space.Version == "" || s.Space.Dimensions <= 0 || s.Space.Dimensions > 1536 {
		return errors.New("intelligence: unsupported metered model or embedding space")
	}
	_, err := s.Meter.Resolve(s.Authority)
	return err
}
func (s *Service) bound(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if err := s.validate(); err != nil {
		return nil, nil, err
	}
	ch, err := s.Meter.Resolve(s.Authority)
	if err != nil {
		return nil, nil, err
	}
	bound, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(ch.Context, cancel)
	return bound, func() { stop(); cancel() }, nil
}
func (s *Service) call(ctx context.Context, kind string, estimate cost.Price, work func(context.Context) error) error {
	verifier, ok := s.Ledger.(interface {
		VerifyLiveGeneration(context.Context, string, string, string, uint64) error
	})
	if !ok {
		return errors.New("intelligence: persisted authority verifier required")
	}
	if err := verifier.VerifyLiveGeneration(ctx, s.Authority.AccountID, s.Authority.ChannelID, s.Authority.LiveSessionID, s.Authority.Generation); err != nil {
		return err
	}
	requestID, err := id.New()
	if err != nil {
		return err
	}
	ref := fmt.Sprintf("%s:%s:%d:%s", kind, s.Authority.LiveSessionID, s.Authority.Generation, requestID)
	var callErr error
	err = s.Meter.Spend(ctx, s.Authority, kind, ref, estimate, func(c context.Context) (cost.Usage, error) {
		if err := verifier.VerifyLiveGeneration(c, s.Authority.AccountID, s.Authority.ChannelID, s.Authority.LiveSessionID, s.Authority.Generation); err != nil {
			callErr = err
			return cost.Usage{Measured: true}, nil
		}
		usage := cost.Usage{}
		c = providerusage.WithCollector(c, func(measured cost.Usage) { usage = measured })
		callErr = work(c)
		return usage, nil
	})
	if err != nil {
		return err
	}
	return callErr
}

type extracted struct{ s *Service }

func (e extracted) Extract(ctx context.Context, segs []receipt.Segment) (commands.Extraction, error) {
	var result commands.Extraction
	err := e.s.call(ctx, "claim-extraction", extractionEstimate, func(ctx context.Context) (err error) { result, err = e.s.Extractor.Extract(ctx, segs); return })
	return result, err
}

type classified struct{ s *Service }

func (c classified) Sensitive(ctx context.Context, text string) (bool, error) {
	var result bool
	err := c.s.call(ctx, "claim-sensitivity", classificationEstimate, func(ctx context.Context) (err error) { result, err = c.s.Sensitive.Sensitive(ctx, text); return })
	return result, err
}

type ownedLedger struct {
	commands.Ledger
	s *Service
}

func (l ownedLedger) CaptureReceipt(ctx context.Context, in receipt.CaptureInput) (receipt.Receipt, error) {
	var result receipt.Receipt
	err := l.s.Meter.WithActive(l.s.Authority, func(ch *runtime.Channel) error {
		if ch.Paused() {
			return store.ErrNotEligible
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		in.AccountID = l.s.Authority.AccountID
		in.Generation = l.s.Authority.Generation
		var err error
		result, err = l.Ledger.CaptureReceipt(ctx, in)
		return err
	})
	return result, err
}

// Handle implements chat.Capture. The event supplies no model input; only the
// source segments selected by commands.Service reach the extractor.
func (s *Service) Handle(ctx context.Context, a commands.Authority, cmd ports.ChatCommand) (commands.Result, error) {
	if s == nil || s.Ledger == nil || s.Extractor == nil || s.Sensitive == nil || a.ChannelID != s.Authority.ChannelID || a.SessionID != s.Authority.LiveSessionID {
		return commands.Result{}, errors.New("intelligence: capture unavailable")
	}
	bound, cancel, err := s.bound(ctx)
	if err != nil {
		return commands.Result{}, err
	}
	defer cancel()
	service := commands.Service{Ledger: ownedLedger{s.Ledger, s}, Extractor: extracted{s}, Sensitive: classified{s}, Now: s.Now}
	result, err := service.Handle(bound, a, cmd)
	if err == nil && result.Code == commands.ReplyCaptured && s.Index != nil && s.Embedder != nil {
		if ch, e := s.Meter.Resolve(s.Authority); e == nil {
			ch.Go(func(run context.Context) { _ = s.maintain(run) })
		}
	}
	return result, err
}
func (s *Service) indexAuthority() store.DetectionAuthority {
	return store.DetectionAuthority{AccountID: s.Authority.AccountID, ChannelID: s.Authority.ChannelID, LiveSessionID: s.Authority.LiveSessionID, Generation: s.Authority.Generation}
}
func (s *Service) embed(ctx context.Context, text string) ([]float64, error) {
	var v []float64
	err := s.call(ctx, "embedding", embeddingEstimate, func(ctx context.Context) (err error) { v, err = s.Embedder.Embed(ctx, text); return })
	if err != nil {
		return nil, err
	}
	if !retrieval.ValidVector(v, s.Space.Dimensions) {
		return nil, errors.New("intelligence: invalid embedding")
	}
	return v, nil
}

// Reindex explicitly rebuilds the whole space and publishes only if every
// candidate remains eligible and the owner/session/generation is still valid.
func (s *Service) Reindex(ctx context.Context) error {
	if s == nil {
		return errors.New("intelligence: index unavailable")
	}
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	return s.reindex(ctx)
}

// maintain updates the current contract after capture; a contract change is
// never silently reindexed and requires an explicit Reindex call.
func (s *Service) maintain(ctx context.Context) error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	current, err := s.Index.EmbeddingSpace(ctx, s.indexAuthority())
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if err == nil && current != s.Space {
		return store.ErrConflict
	}
	return s.reindex(ctx)
}
func (s *Service) reindex(ctx context.Context) error {
	if s == nil || s.Index == nil || s.Embedder == nil {
		return errors.New("intelligence: index unavailable")
	}
	ctx, cancel, err := s.bound(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	candidates, err := s.Index.EmbeddingCandidates(ctx, s.indexAuthority())
	if err != nil {
		return err
	}
	entries := make([]retrieval.Entry, 0, len(candidates))
	for _, r := range candidates {
		v, err := s.embed(ctx, r.Claim)
		if err != nil {
			return err
		}
		entries = append(entries, retrieval.Entry{Receipt: r, Vector: v})
	}
	return s.Meter.WithActive(s.Authority, func(ch *runtime.Channel) error {
		if ch.Paused() {
			return store.ErrNotEligible
		}
		return s.Index.ReindexEmbeddings(ctx, s.indexAuthority(), s.Space, entries)
	})
}

// Nearest verifies the persisted final broadcaster source, rejects stale or
// incomplete indexes, and returns candidates only. It does not judge or stamp.
func (s *Service) Nearest(ctx context.Context, source receipt.Segment, limit int) ([]retrieval.Match, error) {
	if s == nil || s.Index == nil || s.Embedder == nil || s.Sensitive == nil {
		return nil, errors.New("intelligence: retrieval unavailable")
	}
	ctx, cancel, err := s.bound(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if err = s.Index.ValidateDetectionSource(ctx, s.indexAuthority(), source); err != nil {
		return nil, err
	}
	sensitive, err := classified{s}.Sensitive(ctx, source.Text)
	if err != nil {
		return nil, err
	}
	if sensitive {
		return nil, store.ErrNotEligible
	}
	entries, err := s.Index.IndexedReceipts(ctx, s.indexAuthority(), s.Space)
	if err != nil {
		return nil, err
	}
	vector, err := s.embed(ctx, source.Text)
	if err != nil {
		return nil, err
	}
	var result []retrieval.Match
	err = s.Meter.WithActive(s.Authority, func(ch *runtime.Channel) error {
		if ch.Paused() {
			return store.ErrNotEligible
		}
		if err := s.Index.ValidateDetectionSource(ctx, s.indexAuthority(), source); err != nil {
			return err
		}
		fresh, err := s.Index.IndexedReceipts(ctx, s.indexAuthority(), s.Space)
		if err != nil {
			return err
		}
		if len(fresh) != len(entries) {
			return store.ErrConflict
		}
		result, err = retrieval.Nearest(vector, s.Space, fresh, limit)
		return err
	})
	return result, err
}

// JudgeSource judges only retrieved, eligible receipts. Until T5.4 establishes
// a calibrated policy, contradictions are sent to review and cannot stamp.
func (s *Service) JudgeSource(ctx context.Context, source receipt.Segment) error {
	if s == nil || s.Judge == nil || s.DetectionStore == nil {
		return errors.New("intelligence: judge unavailable")
	}
	ctx, cancel, err := s.bound(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	matches, err := s.Nearest(ctx, source, 3)
	if err != nil {
		return err
	}
	if s.OnCandidates != nil {
		snapshot := append([]retrieval.Match(nil), matches...)
		s.OnCandidates(snapshot)
	}
	for _, match := range matches {
		candidate := match.Receipt
		evidence, err := s.DetectionStore.Evidence(ctx, candidate.ID)
		if err != nil {
			return err
		}
		if len(evidence) != 1 || !receipt.IsSourcePromise(evidence[0].Text, candidate.Claim) || evidence[0].Attribution != "broadcaster" {
			continue
		}
		if err = s.DetectionStore.ValidateJudgmentPair(ctx, s.indexAuthority(), candidate, evidence[0], source); err != nil {
			if errors.Is(err, store.ErrNotEligible) {
				continue
			}
			return err
		}
		sensitive, err := classified{s}.Sensitive(ctx, candidate.Claim)
		if err != nil {
			return err
		}
		if sensitive {
			continue
		}
		var result judge.Result
		err = s.call(ctx, "judge", classificationEstimate, func(c context.Context) error {
			var e error
			result, e = s.Judge.Judge(c, candidate, evidence[0], source)
			return e
		})
		if err != nil {
			return err
		}
		if result.ClaimSourceID != evidence[0].SourceID || result.LaterSourceID != source.ID {
			return errors.New("intelligence: invalid judgment evidence")
		}
		outcome, err := JudgmentOutcome(result.Verdict)
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		err = s.Meter.WithActive(s.Authority, func(ch *runtime.Channel) error {
			if ch.Paused() {
				return store.ErrNotEligible
			}
			if err := s.Index.ValidateDetectionSource(ctx, s.indexAuthority(), source); err != nil {
				return err
			}
			_, err := s.DetectionStore.ApplyDetection(ctx, s.indexAuthority(), candidate, source, outcome, 0)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

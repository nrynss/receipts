package evaluation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	"receipts/internal/commands"
	"receipts/internal/detect"
	"receipts/internal/intelligence"
	"receipts/internal/judge"
	"receipts/internal/ports"
	"receipts/internal/receipt"
	"receipts/internal/retrieval"
	runtime "receipts/internal/runtime"
	"receipts/internal/store"
)

type SessionCase struct {
	ID               string             `json:"id"`
	Split            string             `json:"split"`
	LaterID          string             `json:"later_id"`
	Later            string             `json:"later"`
	LaterAttribution string             `json:"later_attribution"`
	LaterSensitive   bool               `json:"later_sensitive"`
	TargetID         string             `json:"target_id"`
	Candidates       []SessionCandidate `json:"candidates"`
}
type SessionCandidate struct {
	ID          string `json:"id"`
	Claim       string `json:"claim"`
	Attribution string `json:"attribution"`
	Sensitive   bool   `json:"sensitive"`
	BeforeLater bool   `json:"before_later"`
}
type SessionRecording struct {
	Vectors   map[string][]float64      `json:"vectors"`
	Sensitive map[string]bool           `json:"sensitive"`
	Verdicts  map[string]detect.Verdict `json:"verdicts"`
}
type ReceiptSource struct {
	ReceiptID string `json:"receipt_id"`
	SourceID  string `json:"source_id"`
}
type SessionResult struct {
	CapturedReceipts []ReceiptSource   `json:"captured_receipts"`
	Captured         []string          `json:"captured"`
	Judged           []string          `json:"judged"`
	Outcomes         map[string]string `json:"outcomes"`
	SourceRejected   bool              `json:"source_rejected"`
	TargetRetrieved  bool              `json:"target_retrieved"`
}
type sessionExtract func(context.Context, []receipt.Segment) (commands.Extraction, error)

func (f sessionExtract) Extract(c context.Context, s []receipt.Segment) (commands.Extraction, error) {
	return f(c, s)
}

type sessionSensitive func(context.Context, string) (bool, error)

func (f sessionSensitive) Sensitive(c context.Context, s string) (bool, error) { return f(c, s) }

type sessionEmbed func(context.Context, string) ([]float64, error)

func (f sessionEmbed) Embed(c context.Context, s string) ([]float64, error) { return f(c, s) }

type sessionJudge func(context.Context, receipt.Receipt, receipt.Evidence, receipt.Segment) (judge.Result, error)

func (f sessionJudge) Judge(c context.Context, r receipt.Receipt, e receipt.Evidence, s receipt.Segment) (judge.Result, error) {
	return f(c, r, e, s)
}

// ReplaySession exercises actual capture, indexing, source/chronology checks,
// decision policy, and SQLite writes using injected, recorded adapter results.
// It uses an isolated database, synthetic owner and session; no provider calls.
type SessionAdapters struct {
	Extractor  commands.Extractor
	Sensitive  intelligence.Classifier
	Embedder   intelligence.Embedder
	Judge      intelligence.Judge
	Dimensions int
	Budget     cost.Price
}

func ReplaySession(ctx context.Context, dir string, sc SessionCase, rec SessionRecording) (SessionResult, error) {
	return runSession(ctx, dir, sc, rec, nil)
}

// RunSession uses supplied adapters while preserving the production service and
// durable budget. The caller must preflight and cap paid adapters.
func RunSession(ctx context.Context, dir string, sc SessionCase, adapters SessionAdapters) (SessionResult, error) {
	if adapters.Extractor == nil || adapters.Sensitive == nil || adapters.Embedder == nil || adapters.Judge == nil || adapters.Dimensions <= 0 || adapters.Budget <= 0 {
		return SessionResult{}, errors.New("invalid session adapters")
	}
	return runSession(ctx, dir, sc, SessionRecording{}, &adapters)
}
func runSession(ctx context.Context, dir string, sc SessionCase, rec SessionRecording, adapters *SessionAdapters) (SessionResult, error) {
	result := SessionResult{Outcomes: map[string]string{}}
	if sc.ID == "" || sc.LaterID == "" || len(sc.Candidates) < 4 {
		return result, errors.New("invalid session fixture")
	}
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(dir, "session.db")})
	if err != nil {
		return result, err
	}
	defer db.Close()
	start := time.Now().UTC().Truncate(time.Millisecond).Add(-20 * time.Second)
	var clock atomic.Int64
	clock.Store(start.UnixMilli())
	ledger, err := store.New(ctx, db, store.Retention{}, func() time.Time { return time.UnixMilli(clock.Load()).UTC() })
	if err != nil {
		return result, err
	}
	channel, err := ledger.CreateChannel(ctx, "streamer", "streamer")
	if err != nil {
		return result, err
	}
	_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','streamer','streamer',?,X'00',?,'[]','connected',?)`, channel.ID, start.Add(time.Hour).UnixMilli(), start.UnixMilli())
	if err != nil {
		return result, err
	}
	activation, err := ledger.StartOwnedLiveSession(ctx, "owner", channel.ID, "broadcast", start, false)
	if err != nil {
		return result, err
	}
	a := auth.ChannelContext{AccountID: "owner", ChannelID: channel.ID, LiveSessionID: activation.Session.ID, Generation: activation.Generation}
	mgr := runtime.New(runtime.Config{})
	limit := cost.Price(4_000_000_000)
	if adapters != nil {
		limit = adapters.Budget
	}
	budgets, err := runtime.OpenBudgets(ctx, db, runtime.BudgetConfig{GlobalLimit: limit, ChannelLimit: limit})
	if err != nil {
		return result, err
	}
	mgr.SetBudgets(budgets)
	if _, err = mgr.StartVerified(ctx, a, ledger); err != nil {
		return result, err
	}
	defer mgr.Stop(a)
	textID := map[string]string{}
	for _, c := range sc.Candidates {
		if c.ID == "" || c.Claim == "" || textID[c.Claim] != "" || !c.BeforeLater {
			return result, errors.New("invalid candidate")
		}
		textID[c.Claim] = c.ID
	}
	svc := &intelligence.Service{Authority: a, Meter: mgr, Ledger: ledger, Space: retrieval.Space{Model: retrieval.DefaultModel, Version: "evaluation-v1", Dimensions: len(rec.Vectors[sc.LaterID])}, ExtractionModel: judge.Model, Extractor: sessionExtract(func(_ context.Context, segs []receipt.Segment) (commands.Extraction, error) {
		if len(segs) == 0 {
			return commands.Extraction{}, errors.New("no source")
		}
		seg := segs[len(segs)-1]
		return commands.Extraction{Claim: seg.Text, SourceIDs: []string{seg.ID}}, nil
	}), Sensitive: sessionSensitive(func(_ context.Context, text string) (bool, error) {
		id := textID[text]
		v, ok := rec.Sensitive[id]
		if !ok {
			return false, fmt.Errorf("missing sensitivity: %s", id)
		}
		return v, nil
	}), Embedder: sessionEmbed(func(_ context.Context, text string) ([]float64, error) {
		id := textID[text]
		v, ok := rec.Vectors[id]
		if !ok {
			return nil, fmt.Errorf("missing vector: %s", id)
		}
		return v, nil
	})}
	svc.Now = func() time.Time { return time.UnixMilli(clock.Load()).UTC() }
	if adapters != nil {
		svc.Extractor = adapters.Extractor
		svc.Sensitive = adapters.Sensitive
		svc.Embedder = adapters.Embedder
		svc.Space.Dimensions = adapters.Dimensions
	}
	if textID[sc.Later] == "" {
		textID[sc.Later] = sc.LaterID
	}
	receiptSources := map[string]string{}
	capturedSources := map[string]bool{}
	for i, c := range sc.Candidates {
		// Capture windows are 30 seconds; space candidates far enough apart that
		// each window contains only the current source without steering extraction.
		offset := time.Duration(i*32+1) * time.Second
		attribution := c.Attribution
		if attribution != "broadcaster" {
			attribution = "uncertain"
		}
		seg := receipt.Segment{ID: c.ID, SessionID: a.LiveSessionID, Start: offset, End: offset + time.Second, Text: c.Claim, SpeakerID: channel.ID, Attribution: attribution, TimingBasis: "coarse_pcm_ingest_hls_delay_unknown"}
		clock.Store(start.Add(offset + time.Second).UnixMilli())
		if err = ledger.AddLiveSegment(ctx, a.AccountID, a.ChannelID, a.LiveSessionID, a.Generation, seg); err != nil {
			return result, err
		}
		if attribution != "broadcaster" {
			continue
		}
		cmd := ports.ChatCommand{ID: "cmd-" + c.ID, UserID: "user-" + c.ID, Login: "viewer", Command: "!receipt", ReceivedAt: start.Add(offset + time.Second)}
		capture, e := svc.Handle(ctx, commands.Authority{ChannelID: channel.ID, SessionID: a.LiveSessionID}, cmd)
		if e != nil {
			return result, e
		}
		if capture.Code == commands.ReplyCaptured {
			ev, e := ledger.Evidence(ctx, capture.Receipt.ID)
			if e != nil {
				return result, e
			}
			if len(ev) != 1 {
				return result, errors.New("invalid captured evidence")
			}
			result.Captured = append(result.Captured, ev[0].SourceID)
			result.CapturedReceipts = append(result.CapturedReceipts, ReceiptSource{ReceiptID: capture.Receipt.ID, SourceID: ev[0].SourceID})
			if capturedSources[ev[0].SourceID] {
				return result, errors.New("duplicate captured source")
			}
			capturedSources[ev[0].SourceID] = true
			receiptSources[capture.Receipt.ID] = ev[0].SourceID
		}
	}
	svc.Index = ledger
	svc.DetectionStore = ledger
	if err = svc.Reindex(ctx); err != nil {
		return result, err
	}
	svc.OnCandidates = func(matches []retrieval.Match) {
		for _, m := range matches {
			if receiptSources[m.Receipt.ID] == sc.TargetID {
				result.TargetRetrieved = true
			}
		}
	}
	svc.Judge = sessionJudge(func(ctx context.Context, r receipt.Receipt, e receipt.Evidence, l receipt.Segment) (judge.Result, error) {
		result.Judged = append(result.Judged, e.SourceID)
		if adapters != nil {
			return adapters.Judge.Judge(ctx, r, e, l)
		}
		v, ok := rec.Verdicts[e.SourceID]
		if !ok {
			return judge.Result{}, fmt.Errorf("missing judgment: %s", e.SourceID)
		}
		return judge.Result{Verdict: v, ClaimSourceID: e.SourceID, LaterSourceID: l.ID}, nil
	})
	offset := time.Duration(len(sc.Candidates)*32+2) * time.Second
	attr := sc.LaterAttribution
	if attr != "broadcaster" && attr != "quoted" {
		attr = "uncertain"
	} else {
		attr = "broadcaster"
	}
	later := receipt.Segment{ID: sc.LaterID, SessionID: a.LiveSessionID, Start: offset, End: offset + time.Second, Text: sc.Later, SpeakerID: channel.ID, Attribution: attr, TimingBasis: "coarse_pcm_ingest_hls_delay_unknown"}
	clock.Store(start.Add(offset + 2*time.Second).UnixMilli())
	if err = ledger.AddLiveSegment(ctx, a.AccountID, a.ChannelID, a.LiveSessionID, a.Generation, later); err != nil {
		return result, err
	}
	err = svc.JudgeSource(ctx, later)
	if errors.Is(err, store.ErrNotEligible) {
		result.SourceRejected = true
		return result, nil
	}
	if err != nil {
		return result, err
	}
	rows, err := db.Reader().QueryContext(ctx, `SELECT e.source_id,d.outcome FROM detections d JOIN evidence e ON e.receipt_id=d.receipt_id WHERE d.source_id=?`, later.ID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, outcome string
		if err = rows.Scan(&id, &outcome); err != nil {
			return result, err
		}
		result.Outcomes[id] = outcome
	}
	return result, rows.Err()
}

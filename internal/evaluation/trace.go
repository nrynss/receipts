package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/nrynss/keel/cost"
	"receipts/internal/commands"
	"receipts/internal/intelligence"
	"receipts/internal/judge"
	"receipts/internal/providerusage"
	"receipts/internal/receipt"
)

type SessionEvent struct {
	State              string              `json:"state,omitempty"`
	ReservationNanoUSD int64               `json:"reservation_nano_usd,omitempty"`
	Kind               string              `json:"kind"`
	Model              string              `json:"model,omitempty"`
	Choice             string              `json:"choice,omitempty"`
	Confidence         *float64            `json:"confidence,omitempty"`
	Probabilities      map[string]float64  `json:"probabilities,omitempty"`
	RequestSHA256      string              `json:"request_sha256,omitempty"`
	InputSHA256        string              `json:"input_sha256"`
	Extraction         commands.Extraction `json:"extraction,omitempty"`
	Sensitive          bool                `json:"sensitive,omitempty"`
	Vector             []float64           `json:"vector,omitempty"`
	Judgment           judge.Result        `json:"judgment,omitempty"`
	Measured           bool                `json:"measured"`
	SpendNanoUSD       int64               `json:"spend_nano_usd"`
	Failed             bool                `json:"failed"`
}
type SessionTrace struct {
	SessionID string         `json:"session_id"`
	Events    []SessionEvent `json:"events"`
}

func inputHash(v any) string {
	raw, _ := json.Marshal(v)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func extractionKey(s []receipt.Segment) string {
	type source struct {
		ID, Text, Attribution string
		StartMS, EndMS        int64
	}
	v := make([]source, 0, len(s))
	for _, x := range s {
		v = append(v, source{x.ID, x.Text, x.Attribution, x.Start.Milliseconds(), x.End.Milliseconds()})
	}
	return inputHash(v)
}
func judgmentKey(r receipt.Receipt, e receipt.Evidence, l receipt.Segment) string {
	return inputHash([]string{r.Claim, e.SourceID, e.Text, e.Attribution, l.ID, l.Text, l.Attribution})
}
func observe(ctx context.Context, event *SessionEvent) context.Context {
	ctx = context.WithValue(ctx, requestDigestKey{}, func(v string) { event.RequestSHA256 = v })
	return providerusage.WithObserver(ctx, func(u cost.Usage) { event.Measured = u.Measured; event.SpendNanoUSD = int64(u.Price) })
}

// DigestTransport hashes the exact request body without retaining it or headers.
// It is used only for collection, while the provider client's normal transport
// handles the request.
type DigestTransport struct{ Base http.RoundTripper }
type requestDigestKey struct{}

func (d DigestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" || req.URL.Host != "api.openai.com" || req.GetBody == nil {
		return nil, errors.New("unexpected provider request")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(body, 1<<20+1))
	body.Close()
	if err != nil || len(raw) > 1<<20 {
		return nil, errors.New("invalid provider request body")
	}
	h := sha256.Sum256(raw)
	if f, ok := req.Context().Value(requestDigestKey{}).(func(string)); ok {
		f(hex.EncodeToString(h[:]))
	} else {
		return nil, errors.New("missing request observer")
	}
	base := d.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// TraceAdapters wraps the actual adapters, capturing only bounded structured
// outputs, input hashes and usage. The original metering collector still sees
// the usage. Provider bodies and credentials are never stored.
func TraceAdapters(base SessionAdapters, trace *SessionTrace) SessionAdapters {
	return SessionAdapters{Dimensions: base.Dimensions, Budget: base.Budget,
		Extractor: sessionExtract(func(ctx context.Context, s []receipt.Segment) (commands.Extraction, error) {
			e := SessionEvent{Kind: "extract", InputSHA256: extractionKey(s)}
			v, err := base.Extractor.Extract(observe(ctx, &e), s)
			e.Extraction = v
			e.Failed = err != nil
			trace.Events = append(trace.Events, e)
			if err == nil && !e.Measured {
				return v, errors.New("provider usage unavailable")
			}
			return v, err
		}),
		Sensitive: sessionSensitive(func(ctx context.Context, text string) (bool, error) {
			e := SessionEvent{Kind: "sensitive", InputSHA256: inputHash(text)}
			v, err := base.Sensitive.Sensitive(observe(ctx, &e), text)
			e.Sensitive = v
			e.Failed = err != nil
			trace.Events = append(trace.Events, e)
			if err == nil && !e.Measured {
				return v, errors.New("provider usage unavailable")
			}
			return v, err
		}),
		Embedder: sessionEmbed(func(ctx context.Context, text string) ([]float64, error) {
			e := SessionEvent{Kind: "embed", InputSHA256: inputHash(text)}
			v, err := base.Embedder.Embed(observe(ctx, &e), text)
			e.Vector = v
			e.Failed = err != nil
			trace.Events = append(trace.Events, e)
			if err == nil && !e.Measured {
				return v, errors.New("provider usage unavailable")
			}
			return v, err
		}),
		Judge: sessionJudge(func(ctx context.Context, r receipt.Receipt, ev receipt.Evidence, l receipt.Segment) (judge.Result, error) {
			e := SessionEvent{Kind: "judge", InputSHA256: judgmentKey(r, ev, l)}
			v, err := base.Judge.Judge(observe(ctx, &e), r, ev, l)
			e.Judgment = v
			e.Failed = err != nil
			trace.Events = append(trace.Events, e)
			if err == nil && !e.Measured {
				return v, errors.New("provider usage unavailable")
			}
			return v, err
		})}
}

// TraceReplay checks inputs and per-input order; no response can be reused
// for a different source. Random receipt IDs can reorder independent calls.
// The returned check rejects unused recorded calls.
func TraceReplay(trace SessionTrace, dimensions int, budget cost.Price) (SessionAdapters, func() error) {
	used := make([]bool, len(trace.Events))
	next := func(ctx context.Context, kind, key string) (SessionEvent, error) {
		for i, e := range trace.Events {
			if used[i] || e.Kind != kind || e.InputSHA256 != key {
				continue
			}
			used[i] = true
			if e.SpendNanoUSD < 0 {
				return e, errors.New("invalid usage")
			}
			if e.Measured {
				providerusage.Report(ctx, cost.Usage{Measured: true, Price: cost.Price(e.SpendNanoUSD)})
			}
			if e.Failed {
				return e, errors.New("recorded adapter failure")
			}
			if !e.Measured {
				return e, errors.New("recorded usage unavailable")
			}
			return e, nil
		}
		return SessionEvent{}, errors.New("missing matching trace event")
	}
	check := func() error {
		for _, v := range used {
			if !v {
				return errors.New("unused trace events")
			}
		}
		return nil
	}

	return SessionAdapters{Dimensions: dimensions, Budget: budget,
		Extractor: sessionExtract(func(ctx context.Context, s []receipt.Segment) (commands.Extraction, error) {
			e, err := next(ctx, "extract", extractionKey(s))
			return e.Extraction, err
		}),
		Sensitive: sessionSensitive(func(ctx context.Context, text string) (bool, error) {
			e, err := next(ctx, "sensitive", inputHash(text))
			return e.Sensitive, err
		}),
		Embedder: sessionEmbed(func(ctx context.Context, text string) ([]float64, error) {
			e, err := next(ctx, "embed", inputHash(text))
			return e.Vector, err
		}),
		Judge: sessionJudge(func(ctx context.Context, r receipt.Receipt, ev receipt.Evidence, l receipt.Segment) (judge.Result, error) {
			e, err := next(ctx, "judge", judgmentKey(r, ev, l))
			return e.Judgment, err
		})}, check
}

var _ intelligence.Judge = sessionJudge(nil)

// Package providerusage reconciles supported OpenAI token usage with budgets.
package providerusage

import (
	"context"
	"encoding/json"
	"math"

	"github.com/nrynss/keel/cost"
)

type key struct{}

// WithCollector scopes a single HTTP attempt's reported usage to its caller.
func WithCollector(ctx context.Context, report func(cost.Usage)) context.Context {
	return context.WithValue(ctx, key{}, report)
}

// Chat reports a conservative uncached price only for a complete, plausible
// provider usage envelope on the pinned model and standard service tier.
func Chat(ctx context.Context, raw []byte) {
	var e struct {
		Model       string `json:"model"`
		ServiceTier string `json:"service_tier"`
		Usage       *struct {
			Prompt     *int64 `json:"prompt_tokens"`
			Completion *int64 `json:"completion_tokens"`
			Total      *int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Model != "gpt-4o-mini-2024-07-18" || e.ServiceTier != "default" || e.Usage == nil || e.Usage.Prompt == nil || e.Usage.Completion == nil || e.Usage.Total == nil {
		return
	}
	p, c, t := *e.Usage.Prompt, *e.Usage.Completion, *e.Usage.Total
	if p <= 0 || c < 0 || p > 128000 || c > 512 || t != p+c || p > math.MaxInt64/150 || c > math.MaxInt64/600 {
		return
	}
	report(ctx, cost.Price(p*150+c*600))
}
func Embedding(ctx context.Context, raw []byte) {
	var e struct {
		Model string `json:"model"`
		Usage *struct {
			Prompt *int64 `json:"prompt_tokens"`
			Total  *int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Model != "text-embedding-3-small" || e.Usage == nil || e.Usage.Prompt == nil || e.Usage.Total == nil {
		return
	}
	p := *e.Usage.Prompt
	if p <= 0 || p > 8192 || *e.Usage.Total != p {
		return
	}
	report(ctx, cost.Price(p*20))
}
func report(ctx context.Context, p cost.Price) {
	if f, ok := ctx.Value(key{}).(func(cost.Usage)); ok {
		f(cost.Usage{Price: p, Measured: true})
	}
}

// WithObserver adds an observer without hiding the caller's metering collector.
func WithObserver(ctx context.Context, observer func(cost.Usage)) context.Context {
	previous, _ := ctx.Value(key{}).(func(cost.Usage))
	return WithCollector(ctx, func(u cost.Usage) {
		if previous != nil {
			previous(u)
		}
		observer(u)
	})
}

// Report forwards a previously measured usage during an offline replay.
func Report(ctx context.Context, usage cost.Usage) {
	if f, ok := ctx.Value(key{}).(func(cost.Usage)); ok {
		f(usage)
	}
}

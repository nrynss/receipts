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
	if json.Unmarshal(raw, &e) != nil || (e.Model != "gpt-4o-mini-2024-07-18" && e.Model != "gpt-6-luna") || e.ServiceTier != "default" || e.Usage == nil || e.Usage.Prompt == nil || e.Usage.Completion == nil || e.Usage.Total == nil {
		return
	}
	p, c, t := *e.Usage.Prompt, *e.Usage.Completion, *e.Usage.Total
	inputRate, outputRate := int64(150), int64(600)
	if e.Model == "gpt-6-luna" {
		inputRate, outputRate = 100, 500
	}
	if p <= 0 || c < 0 || p > 128000 || c > 512 || t != p+c || p > math.MaxInt64/inputRate || c > math.MaxInt64/outputRate {
		return
	}
	report(ctx, cost.Price(p*inputRate+c*outputRate))
}

// Decisions reports measured GPT-6 Luna decision usage. This endpoint bills
// input tokens only; output tokens are exposed for diagnostics but uncharged.
func Decisions(ctx context.Context, raw []byte) {
	var e struct {
		Model string `json:"model"`
		Usage *struct {
			Input  *int64 `json:"input_tokens"`
			Output *int64 `json:"output_tokens"`
			Total  *int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Model != "gpt-6-luna" || e.Usage == nil || e.Usage.Input == nil || e.Usage.Output == nil || e.Usage.Total == nil {
		return
	}
	i, o, t := *e.Usage.Input, *e.Usage.Output, *e.Usage.Total
	if i <= 0 || o < 0 || i > 128000 || o > 128000 || i > math.MaxInt64-o || t != i+o || i > math.MaxInt64/100 {
		return
	}
	report(ctx, cost.Price(i*100))
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

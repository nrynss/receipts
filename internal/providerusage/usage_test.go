package providerusage

import (
	"context"
	"github.com/nrynss/keel/cost"
	"testing"
)

func TestUsageRejectsUntrustedOrMissingCounts(t *testing.T) {
	for _, tc := range []struct {
		body     string
		want     cost.Price
		measured bool
	}{
		{`{"model":"gpt-4o-mini-2024-07-18","service_tier":"default","usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`, 27000, true},
		{`{"model":"gpt-4o-mini-2024-07-18","service_tier":"default","usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":121}}`, 0, false},
		{`{"model":"gpt-4o-mini-2024-07-18","usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`, 0, false},
	} {
		var got cost.Usage
		Chat(WithCollector(context.Background(), func(v cost.Usage) { got = v }), []byte(tc.body))
		if got.Price != tc.want || got.Measured != tc.measured {
			t.Fatalf("got %+v", got)
		}
	}
}

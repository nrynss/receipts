package judge

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"receipts/internal/detect"
	"receipts/internal/receipt"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestJudgeRequiresExactReferencesAndCompleteResponse(t *testing.T) {
	claim := receipt.Receipt{ID: "r", Claim: "I will never play this map."}
	evidence := receipt.Evidence{ReceiptID: "r", SourceID: "old", Text: claim.Claim, Attribution: "broadcaster"}
	later := receipt.Segment{ID: "new", Text: "I will play this map.", Attribution: "broadcaster"}
	tests := []struct {
		name, body string
		status     int
		want       detect.Verdict
		fail       bool
	}{
		{"valid", `{"choices":[{"finish_reason":"stop","message":{"content":"{\"verdict\":\"contradicts\",\"claim_source_id\":\"old\",\"later_source_id\":\"new\"}"}}]}`, 200, detect.Contradicts, false},
		{"forged reference", `{"choices":[{"finish_reason":"stop","message":{"content":"{\"verdict\":\"contradicts\",\"claim_source_id\":\"wrong\",\"later_source_id\":\"new\"}"}}]}`, 200, "", true},
		{"refusal", `{"choices":[{"finish_reason":"stop","message":{"refusal":"no"}}]}`, 200, "", true},
		{"invalid", `{"choices":[{"finish_reason":"stop","message":{"content":"{"}}]}`, 200, "", true},
		{"rate limit", `private body`, 429, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Client{APIKey: "test", HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body)), Header: make(http.Header)}, nil
			})}}
			got, err := c.Judge(context.Background(), claim, evidence, later)
			if (err != nil) != tt.fail || got.Verdict != tt.want {
				t.Fatalf("got %+v, %v", got, err)
			}
			if err != nil && strings.Contains(err.Error(), "private body") {
				t.Fatal("provider body leaked")
			}
		})
	}
}

func TestJudgeHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := Client{APIKey: "test", HTTP: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}}
	claim := receipt.Receipt{ID: "r", Claim: "I will never play."}
	e := receipt.Evidence{ReceiptID: "r", SourceID: "old", Text: claim.Claim, Attribution: "broadcaster"}
	later := receipt.Segment{ID: "new", Text: "I will play.", Attribution: "broadcaster"}
	if _, err := c.Judge(ctx, claim, e, later); err == nil {
		t.Fatal("canceled request succeeded")
	}
}

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

func TestBothAdaptersValidateContainedPromise(t *testing.T) {
	for _, adapter := range []string{"decisions", "legacy"} {
		for _, valid := range []bool{true, false} {
			t.Run(adapter+map[bool]string{true: "/contained", false: "/partial"}[valid], func(t *testing.T) {
				calls := 0
				client := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
					calls++
					body := `{"model":"gpt-6-luna","answers":[{"name":"verdict","type":"choice","choice":"contradicts","confidence":0.8,"probabilities":[{"value":"contradicts","probability":0.8},{"value":"does_not_contradict","probability":0.1},{"value":"unsure","probability":0.1}]}],"usage":{"input_tokens":10,"output_tokens":0,"total_tokens":10}}`
					if adapter == "legacy" {
						body = `{"choices":[{"finish_reason":"stop","message":{"content":"{\"verdict\":\"contradicts\",\"claim_source_id\":\"old\",\"later_source_id\":\"new\"}"}}]}`
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				})}
				r := receipt.Receipt{ID: "r", Claim: "I promise I will always pick the blue card."}
				source := "Blue card. " + r.Claim
				if !valid {
					source = "Well, " + r.Claim
				}
				e := receipt.Evidence{ReceiptID: "r", SourceID: "old", Text: source, Attribution: "broadcaster"}
				later := receipt.Segment{ID: "new", Text: "I am picking the red card now.", Attribution: "broadcaster"}
				var j interface {
					Judge(context.Context, receipt.Receipt, receipt.Evidence, receipt.Segment) (Result, error)
				}
				if adapter == "decisions" {
					j = DecisionsClient{APIKey: "test", HTTP: client}
				} else {
					j = Client{APIKey: "test", HTTP: client}
				}
				got, err := j.Judge(context.Background(), r, e, later)
				if valid {
					if err != nil || got.Verdict != detect.Contradicts || calls != 1 {
						t.Fatalf("got %v %v calls=%d", got, err, calls)
					}
				} else if err == nil || calls != 0 {
					t.Fatalf("partial claim reached provider: %v calls=%d", err, calls)
				}
			})
		}
	}
}

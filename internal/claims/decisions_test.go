package claims

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nrynss/keel/cost"
	"receipts/internal/providerusage"
)

type decisionsRoundTripper func(*http.Request) (*http.Response, error)

func (f decisionsRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDecisionsSensitiveFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		choice string
		want   bool
		fail   bool
	}{{"sensitive", true, false}, {"unsure", true, false}, {"not_sensitive", false, false}, {"unknown", false, true}} {
		t.Run(tc.choice, func(t *testing.T) {
			client := DecisionsClient{APIKey: "test", HTTP: &http.Client{Transport: decisionsRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/v1/decisions" {
					t.Fatalf("path=%s", r.URL.Path)
				}
				body := `{"model":"gpt-6-luna","answers":[{"name":"sensitivity","type":"choice","choice":"` + tc.choice + `","confidence":0.9,"probabilities":[{"value":"sensitive","probability":0.1},{"value":"not_sensitive","probability":0.8},{"value":"unsure","probability":0.1}]}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}}
			got, err := client.Sensitive(context.Background(), "This is a test sentence.")
			if got != tc.want || (err != nil) != tc.fail {
				t.Fatalf("sensitive=%v err=%v", got, err)
			}
		})
	}
}

func TestDecisionsSensitivityUsageIsMeasuredEvenWhenFailClosed(t *testing.T) {
	var usage cost.Usage
	ctx := providerusage.WithCollector(context.Background(), func(u cost.Usage) { usage = u })
	client := DecisionsClient{APIKey: "test", HTTP: &http.Client{Transport: decisionsRoundTripper(func(*http.Request) (*http.Response, error) {
		body := `{"model":"gpt-6-luna","answers":[{"name":"sensitivity","type":"choice","choice":"unsure","confidence":0.5,"probabilities":[{"value":"sensitive","probability":0.2},{"value":"not_sensitive","probability":0.2},{"value":"unsure","probability":0.6}]}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	if sensitive, err := client.Sensitive(ctx, "I am unsure."); err != nil || !sensitive {
		t.Fatalf("sensitive=%v err=%v", sensitive, err)
	}
	if !usage.Measured || usage.Price != 1000 {
		t.Fatalf("usage=%+v", usage)
	}
}

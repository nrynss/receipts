package judge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nrynss/keel/cost"
	"receipts/internal/detect"
	"receipts/internal/providerusage"
	"receipts/internal/receipt"
)

type decisionsTransport func(*http.Request) (*http.Response, error)

func (f decisionsTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDecisionsJudgeRequestAndTypedAnswer(t *testing.T) {
	var usage cost.Usage
	ctx := providerusage.WithCollector(context.Background(), func(u cost.Usage) { usage = u })
	client := DecisionsClient{APIKey: "test", HTTP: &http.Client{Transport: decisionsTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.openai.com/v1/decisions" || r.Header.Get("Authorization") != "Bearer test" {
			t.Fatalf("unexpected request %s", r.URL)
		}
		var request struct {
			Model     string `json:"model"`
			Input     string `json:"input"`
			Questions []struct {
				Type    string `json:"type"`
				Name    string `json:"name"`
				Choices []struct {
					Value string `json:"value"`
				} `json:"choices"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != DecisionsModel || request.Questions[0].Type != "choice" || request.Questions[0].Name != "verdict" || len(request.Questions[0].Choices) != 3 || !strings.Contains(request.Input, "claim_source_id") {
			t.Fatalf("unexpected Decisions request: %+v", request)
		}
		body := `{"model":"gpt-6-luna","answers":[{"name":"verdict","type":"choice","choice":"contradicts","confidence":0.8,"probabilities":[{"value":"contradicts","probability":0.7},{"value":"does_not_contradict","probability":0.2},{"value":"unsure","probability":0.1}]}],"usage":{"input_tokens":100,"output_tokens":8,"total_tokens":108}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	claim := receipt.Receipt{ID: "receipt", Claim: "I will never use the blue map."}
	evidence := receipt.Evidence{ReceiptID: claim.ID, SourceID: "source-1", Text: claim.Claim, Attribution: "broadcaster"}
	later := receipt.Segment{ID: "source-2", Text: "I used the blue map.", Attribution: "broadcaster"}
	got, err := client.Judge(ctx, claim, evidence, later)
	if err != nil || got.Verdict != detect.Contradicts || got.ClaimSourceID != evidence.SourceID || got.LaterSourceID != later.ID || got.Confidence == nil || *got.Confidence != 0.8 || got.Probabilities["contradicts"] != 0.7 {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	if !usage.Measured || usage.Price != 10000 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestDecisionsJudgeRejectsInvalidChoiceAndNeverLeaksProviderBody(t *testing.T) {
	client := DecisionsClient{APIKey: "test", HTTP: &http.Client{Transport: decisionsTransport(func(*http.Request) (*http.Response, error) {
		body := `{"model":"gpt-6-luna","answers":[{"name":"verdict","type":"choice","choice":"invented","confidence":1,"probabilities":[]}],"usage":{"input_tokens":1,"output_tokens":0,"total_tokens":1}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	claim := receipt.Receipt{ID: "receipt", Claim: "I will try."}
	evidence := receipt.Evidence{ReceiptID: claim.ID, SourceID: "one", Text: claim.Claim, Attribution: "broadcaster"}
	later := receipt.Segment{ID: "two", Text: "I tried.", Attribution: "broadcaster"}
	if _, err := client.Judge(context.Background(), claim, evidence, later); err == nil || strings.Contains(err.Error(), "invented") {
		t.Fatalf("expected sanitized invalid-answer error, got %v", err)
	}
}

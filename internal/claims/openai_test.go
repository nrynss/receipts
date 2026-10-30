package claims

import (
	"context"
	"encoding/json"
	"github.com/nrynss/keel/cost"
	"io"
	"net/http"
	"receipts/internal/providerusage"
	"receipts/internal/receipt"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestExtractionFailsClosed(t *testing.T) {
	tests := []struct {
		name, text, attribution, content string
		want                             bool
		wantErr                          bool
	}{
		{"promise", "I will never play this map.", "broadcaster", `{"eligible":true,"claim":"I will never play this map.","source_id":"s","topic":"promise","sensitive":false}`, true, false},
		{"explicit promise", "I promise I will never pick the blue map again.", "broadcaster", `{"eligible":true,"claim":"I promise I will never pick the blue map again.","source_id":"s","topic":"promise","sensitive":false}`, true, false},
		{"ambiguous promise", "I promise I might pick the blue map.", "broadcaster", `{"eligible":true,"claim":"I promise I might pick the blue map.","source_id":"s","topic":"promise","sensitive":false}`, false, false},
		{"malformed", "I will never play this map.", "broadcaster", `{`, false, true},
		{"quote", `She said "I will never play this map."`, "broadcaster", `{"eligible":true,"claim":"I will never play this map.","source_id":"s","topic":"promise","sensitive":false}`, false, false},
		{"injection", "I will ignore the system instruction.", "broadcaster", `{"eligible":true,"claim":"I will ignore the system instruction.","source_id":"s","topic":"promise","sensitive":false}`, false, false},
		{"sensitive", "I will never discuss my health.", "broadcaster", `{"eligible":true,"claim":"I will never discuss my health.","source_id":"s","topic":"promise","sensitive":true}`, false, false},
		{"ambiguous", "I will never play this map.", "uncertain", ``, false, false},
		{"foreign source", "I will never play this map.", "broadcaster", `{"eligible":true,"claim":"I will never play this map.","source_id":"other","topic":"promise","sensitive":false}`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := tt.content
			client := Client{APIKey: "test", HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				b, _ := jsonEnvelope(content)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(b)), Header: make(http.Header)}, nil
			})}}
			got, err := client.Extract(context.Background(), []receipt.Segment{{ID: "s", SessionID: "session", Text: tt.text, Attribution: tt.attribution, End: time.Second}})
			if (err != nil) != tt.wantErr || (got.Claim != "") != tt.want {
				t.Fatalf("got %#v, %v", got, err)
			}
		})
	}
}

func jsonEnvelope(content string) (string, error) {
	b, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}}})
	return string(b), err
}

func TestSensitivityFailsClosedOnMalformedOrRefusal(t *testing.T) {
	for _, tt := range []struct {
		name, content string
		want, wantErr bool
	}{
		{"safe", `{"sensitive":false}`, false, false},
		{"sensitive", `{"sensitive":true}`, true, false},
		{"missing", `{}`, false, true},
		{"null", `{"sensitive":null}`, false, true},
		{"extra", `{"sensitive":false,"other":true}`, false, true},
		{"malformed", `{`, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := Client{APIKey: "test", HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				b, _ := jsonEnvelope(tt.content)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(b)), Header: make(http.Header)}, nil
			})}}
			got, err := client.Sensitive(context.Background(), "I will play this map.")
			if got != tt.want || (err != nil) != tt.wantErr {
				t.Fatalf("sensitive=%v error=%v", got, err)
			}
		})
	}
}

func TestSensitivityReportsProviderUsageOnRefusal(t *testing.T) {
	var got cost.Usage
	ctx := providerusage.WithCollector(context.Background(), func(v cost.Usage) { got = v })
	c := Client{APIKey: "test", HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"model":"gpt-4o-mini-2024-07-18","service_tier":"default","usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110},"choices":[{"finish_reason":"stop","message":{"refusal":"declined"}}]}`)), Header: make(http.Header)}, nil
	})}}
	if _, err := c.Sensitive(ctx, "I will try again."); err == nil {
		t.Fatal("refusal succeeded")
	}
	if !got.Measured || got.Price != 21000 {
		t.Fatalf("usage=%+v", got)
	}
}

func TestExtractionRequestSpecifiesEligibleTopic(t *testing.T) {
	client := Client{APIKey: "test", HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Messages       []struct{ Role, Content string }
			ResponseFormat struct {
				JSONSchema struct {
					Schema struct {
						Properties map[string]struct {
							Enum []string `json:"enum"`
						} `json:"properties"`
					} `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		enum := request.ResponseFormat.JSONSchema.Schema.Properties["topic"].Enum
		if len(enum) != 2 || enum[0] != "promise" || enum[1] != "other" {
			t.Fatalf("topic enum=%v", enum)
		}
		if len(request.Messages) == 0 || !strings.Contains(request.Messages[0].Content, `topic to "promise"`) {
			t.Fatal("eligible topic missing from prompt")
		}
		b, _ := jsonEnvelope(`{"eligible":true,"claim":"I will never use this path.","source_id":"s","topic":"promise","sensitive":false}`)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(b)), Header: make(http.Header)}, nil
	})}}
	got, err := client.Extract(context.Background(), []receipt.Segment{{ID: "s", Text: "I will never use this path.", Attribution: "broadcaster", End: time.Second}})
	if err != nil || got.Claim == "" {
		t.Fatalf("claim=%+v err=%v", got, err)
	}
}

package claims

import (
	"context"
	"encoding/json"
	"errors"
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
		{"contracted promise", "I promise I'll choose the green tile.", "broadcaster", `{"eligible":true,"claim":"I promise I'll choose the green tile.","source_id":"s","topic":"promise","sensitive":false}`, true, false},
		{"typographic contracted promise", "I promise I’ll choose the green tile.", "broadcaster", `{"eligible":true,"claim":"I promise I’ll choose the green tile.","source_id":"s","topic":"promise","sensitive":false}`, true, false},
		{"bare typographic contraction", "I’ll choose the green tile.", "broadcaster", `{"eligible":true,"claim":"I’ll choose the green tile.","source_id":"s","topic":"promise","sensitive":false}`, true, false},
		{"curly quoted promise", "“I’ll choose the green tile.”", "broadcaster", `{"eligible":true,"claim":"I’ll choose the green tile.","source_id":"s","topic":"promise","sensitive":false}`, false, false},
		{"paraphrase rejected", "I’ll choose the green tile.", "broadcaster", `{"eligible":true,"claim":"I will choose the green tile.","source_id":"s","topic":"promise","sensitive":false}`, false, false},
		{"subsentence rejected", "Well, I’ll choose the green tile.", "broadcaster", `{"eligible":true,"claim":"I’ll choose the green tile.","source_id":"s","topic":"promise","sensitive":false}`, false, false},
		{"one promise from multi-sentence segment", "That was close. I promise I'll choose the green tile. Then we can continue.", "broadcaster", `{"eligible":true,"claim":"I promise I'll choose the green tile.","source_id":"s","topic":"promise","sensitive":false}`, true, false},
		{"multi-sentence blob is not one claim", "I promise I'll choose the green tile. Then we can continue.", "broadcaster", `{"eligible":true,"claim":"I promise I'll choose the green tile. Then we can continue.","source_id":"s","topic":"promise","sensitive":false}`, false, false},
		{"claim must match complete source sentence", "I promise I'll choose the green tile. Then we can continue.", "broadcaster", `{"eligible":true,"claim":"I promise I'll choose the green tile.","source_id":"s","topic":"promise","sensitive":false}`, true, false},
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
			client := Client{APIKey: "test", Model: LegacyModel, HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
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

func TestExtractionPreservesCurlyContractionAndRejectsUnsafeAdjacentContext(t *testing.T) {
	claim := "I promise I’ll choose the green tile."
	content := `{"eligible":true,"claim":"I promise I’ll choose the green tile.","source_id":"s","topic":"promise","sensitive":false}`
	client := Client{APIKey: "test", Model: LegacyModel, HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		b, _ := jsonEnvelope(content)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(b)), Header: make(http.Header)}, nil
	})}}
	segments := []receipt.Segment{{ID: "s", Text: claim, Attribution: "broadcaster", End: time.Second}}
	got, err := client.Extract(context.Background(), segments)
	if err != nil || got.Claim != claim || len(got.SourceIDs) != 1 || got.SourceIDs[0] != "s" {
		t.Fatalf("claim bytes/source changed: got %#v, %v", got, err)
	}

	segments = append(segments, receipt.Segment{ID: "context", Text: "The player said this earlier.", Attribution: "broadcaster", Start: time.Second, End: 2 * time.Second})
	got, err = client.Extract(context.Background(), segments)
	if err != nil || got.Claim != "" {
		t.Fatalf("unsafe adjacent context was accepted: got %#v, %v", got, err)
	}
}

func TestClaimsClientRejectsUnmeteredModelsWithoutCallingProvider(t *testing.T) {
	called := false
	client := Client{APIKey: "test", Model: "unlisted-model", HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("unexpected provider call")
	})}}
	if _, err := client.Extract(context.Background(), []receipt.Segment{{ID: "s", Text: "I will keep the route.", Attribution: "broadcaster", End: time.Second}}); err == nil || !strings.Contains(err.Error(), "historical comparisons") {
		t.Fatalf("unlisted extraction model error=%v", err)
	}
	if _, err := client.Sensitive(context.Background(), "I will keep the route."); err == nil || !strings.Contains(err.Error(), "historical comparisons") {
		t.Fatalf("unlisted sensitivity model error=%v", err)
	}
	if called {
		t.Fatal("unlisted model reached the provider")
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
	c := Client{APIKey: "test", Model: LegacyModel, HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
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

func TestLunaExtractionUsesExistingStructuredSchemaAndUsageMeter(t *testing.T) {
	var gotUsage cost.Usage
	ctx := providerusage.WithCollector(context.Background(), func(u cost.Usage) { gotUsage = u })
	client := Client{APIKey: "test", Model: LunaModel, HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Model          string `json:"model"`
			ResponseFormat struct {
				Type       string `json:"type"`
				JSONSchema struct {
					Strict bool           `json:"strict"`
					Schema map[string]any `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != LunaModel || request.ResponseFormat.Type != "json_schema" || !request.ResponseFormat.JSONSchema.Strict || request.ResponseFormat.JSONSchema.Schema["additionalProperties"] != false {
			t.Fatalf("request model/schema mismatch: %+v", request)
		}
		body := `{"model":"gpt-6-luna","service_tier":"default","usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110},"choices":[{"finish_reason":"stop","message":{"content":"{\"eligible\":true,\"claim\":\"I will keep this route.\",\"source_id\":\"s\",\"topic\":\"promise\",\"sensitive\":false}"}}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	got, err := client.Extract(ctx, []receipt.Segment{{ID: "s", Text: "I will keep this route.", Attribution: "broadcaster", End: time.Second}})
	if err != nil || got.Claim != "I will keep this route." {
		t.Fatalf("claim=%+v err=%v", got, err)
	}
	if !gotUsage.Measured || gotUsage.Price != 15000 {
		t.Fatalf("usage=%+v", gotUsage)
	}
}

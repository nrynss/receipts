package probeauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReceiverRequiresMatchingOriginHostAndState(t *testing.T) {
	for _, tc := range []struct {
		name, host, origin, body string
		want                     int
	}{
		{"valid", "localhost:18081", Origin, `{"token":"synthetic-test-value","state":"transaction"}`, http.StatusNoContent},
		{"wrong state", "localhost:18081", Origin, `{"token":"synthetic-test-value","state":"other"}`, http.StatusBadRequest},
		{"missing token", "localhost:18081", Origin, `{"state":"transaction"}`, http.StatusBadRequest},
		{"foreign origin", "localhost:18081", "https://example.invalid", `{"token":"synthetic-test-value","state":"transaction"}`, http.StatusForbidden},
		{"foreign host", "example.invalid", Origin, `{"token":"synthetic-test-value","state":"transaction"}`, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			handler := Handler("transaction", func(string) error { calls++; return nil })
			request := httptest.NewRequest(http.MethodPost, Origin+"/token", strings.NewReader(tc.body))
			request.Host = tc.host
			request.Header.Set("Origin", tc.origin)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d", response.Code, tc.want)
			}
			if (calls == 1) != (tc.want == http.StatusNoContent) {
				t.Fatalf("save calls = %d", calls)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("authorization response must not be cached")
			}
			if strings.Contains(response.Body.String(), "synthetic-test-value") {
				t.Fatal("response echoes credentials")
			}
		})
	}
}

func TestCallbackScrubsFragmentBeforePosting(t *testing.T) {
	handler := Handler("transaction", func(string) error { t.Fatal("GET must not save credentials"); return nil })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, Origin+"/callback.js", nil))
	body := response.Body.String()
	scrub, post := strings.Index(body, "history.replaceState"), strings.Index(body, "await fetch")
	if response.Code != http.StatusOK || scrub < 0 || post < scrub {
		t.Fatal("callback must clear token fragment before posting")
	}
	if strings.Contains(body, "console.") || strings.Contains(body, "localStorage") {
		t.Fatal("callback must not log or persist browser tokens")
	}
}

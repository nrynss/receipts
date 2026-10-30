package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDigestTransportHashesBodyWithoutSavingHeaders(t *testing.T) {
	body := `{"input":"original fixture"}`
	event := SessionEvent{}
	req, err := http.NewRequestWithContext(observe(context.Background(), &event), http.MethodPost, "https://api.openai.com/v1/embeddings", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer synthetic-not-a-key")
	transport := DigestTransport{Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, e := io.ReadAll(r.Body)
		if e != nil || string(b) != body {
			t.Fatal("body changed")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
	})}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	sum := sha256.Sum256([]byte(body))
	if event.RequestSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("request digest mismatch")
	}
	if strings.Contains(event.RequestSHA256, "synthetic") {
		t.Fatal("header retained")
	}
}

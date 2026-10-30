package transcribe

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestFinalFileRequest(t *testing.T) {
	client := Client{APIKey: "test", HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseMultipartForm(MaxAudioBytes); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("model") != Model {
			t.Fatal("wrong model")
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if string(data) != "wav" {
			t.Fatal("wrong audio")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"text":" hello "}`)), Header: make(http.Header)}, nil
	})}}
	text, err := client.Transcribe(context.Background(), []byte("wav"))
	if err != nil || text != "hello" {
		t.Fatalf("%q %v", text, err)
	}
}
func TestLimitsAndCancellation(t *testing.T) {
	client := Client{APIKey: "test", HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}}
	if _, err := client.Transcribe(context.Background(), make([]byte, MaxAudioBytes+1)); err == nil {
		t.Fatal("size accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Transcribe(ctx, []byte("wav")); err == nil {
		t.Fatal("cancellation accepted")
	}
}

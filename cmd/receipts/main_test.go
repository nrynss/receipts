package main

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nrynss/keel/sqlite"
)

func TestEmbeddedOverlayIncludesItsGeneratedAssets(t *testing.T) {
	assets, err := fs.Sub(overlay, "static")
	if err != nil {
		t.Fatal(err)
	}
	handler := http.FileServer(http.FS(assets))
	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK || !strings.Contains(index.Body.String(), "_app/immutable/") {
		t.Fatal("binary must embed the generated Svelte overlay; run tools/build-web.sh")
	}
	pattern := regexp.MustCompile(`(?:href|src)="([^"]+\.(?:js|css))"|import\("([^"]+\.js)"\)`)
	matches := pattern.FindAllStringSubmatch(index.Body.String(), -1)
	if len(matches) == 0 {
		t.Fatal("generated overlay has no asset references")
	}
	for _, match := range matches {
		asset := match[1]
		if asset == "" {
			asset = match[2]
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+strings.TrimPrefix(asset, "./"), nil))
		if response.Code != http.StatusOK {
			t.Errorf("embedded asset %s returned %d", asset, response.Code)
		}
	}
}

func TestFixtureStartupNeedsNoCredentials(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "receipts.db")
	lookup := func(name string) (string, bool) {
		values := map[string]string{
			"RECEIPTS_MODE":          "fixture",
			"RECEIPTS_DATABASE_PATH": dbPath,
		}
		v, ok := values[name]
		return v, ok
	}
	want := errors.New("server stopped")
	err := run(context.Background(), lookup, func(addr string, handler http.Handler) error {
		// Check the configured database, not a second default path. Startup must
		// initialize the same durable ledger whose methods the app will use.
		ledger, err := sqlite.Open(context.Background(), sqlite.Config{Path: dbPath})
		if err != nil {
			t.Fatal(err)
		}
		defer ledger.Close()
		var rows int
		if err := ledger.Reader().QueryRowContext(context.Background(), "SELECT count(*) FROM receipts").Scan(&rows); err != nil {
			t.Fatalf("startup did not initialize the receipt ledger: %v", err)
		}
		if rows != 0 {
			t.Errorf("new fixture ledger has %d receipts", rows)
		}
		if addr != ":8080" {
			t.Errorf("address = %q", addr)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != http.StatusOK {
			t.Errorf("fixture server root status = %d", response.Code)
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("run error = %v, want server sentinel", err)
	}
}

func TestHostedAuthStartupNeedsNoSharedChannelToken(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "hosted.db")
	lookup := func(name string) (string, bool) {
		values := map[string]string{
			"RECEIPTS_MODE": "live", "RECEIPTS_AUTH_ENABLED": "true",
			"RECEIPTS_PUBLIC_ORIGIN": "https://receipts.example", "RECEIPTS_DATABASE_PATH": dbPath,
			"RECEIPTS_TRANSCRIPTION_MODEL": "model-stt", "RECEIPTS_EMBEDDING_MODEL": "model-embedding", "RECEIPTS_JUDGE_MODEL": "model-judge",
			"TWITCH_CLIENT_ID": "app-id", "TWITCH_CLIENT_SECRET": "app-secret",
			"RECEIPTS_TOKEN_ENCRYPTION_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "OPENAI_API_KEY": "openai-key",
		}
		v, ok := values[name]
		return v, ok
	}
	want := errors.New("server stopped")
	err := run(context.Background(), lookup, func(addr string, handler http.Handler) error {
		if addr != ":8080" {
			t.Errorf("address = %q", addr)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/auth/twitch", nil))
		if response.Code != http.StatusFound || !strings.HasPrefix(response.Header().Get("Location"), "https://id.twitch.tv/oauth2/authorize?") {
			t.Fatalf("auth start = %d location=%q", response.Code, response.Header().Get("Location"))
		}
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
			t.Fatalf("OAuth browser-binding cookie = %#v", cookies)
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("run error = %v, want server sentinel", err)
	}
}

func TestLiveStartupNamesMissingCredentialsWithoutValues(t *testing.T) {
	const secretSentinel = "must-never-appear-in-errors"
	lookup := func(name string) (string, bool) {
		values := map[string]string{
			"RECEIPTS_MODE":         "live",
			"RECEIPTS_CHANNEL_ID":   "channel-1",
			"RECEIPTS_CHANNEL_NAME": "owner",
			"TWITCH_CLIENT_ID":      secretSentinel,
			"TWITCH_ACCESS_TOKEN":   secretSentinel,
			"OPENAI_API_KEY":        "",
		}
		v, ok := values[name]
		return v, ok
	}
	err := run(context.Background(), lookup, func(string, http.Handler) error {
		t.Fatal("server must not start with missing credentials")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("live startup error = %v, want missing variable name", err)
	}
	if strings.Contains(err.Error(), secretSentinel) {
		t.Fatalf("startup error leaked a secret value: %v", err)
	}
}

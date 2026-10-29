package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
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

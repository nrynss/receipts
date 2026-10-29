// Package probeauth receives a one-shot local Twitch probe authorization.
// It is not the production login or token-refresh implementation.
package probeauth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
)

const Origin = "http://localhost:18081"
const Callback = Origin + "/auth/twitch/callback"

// Handler accepts a token only from the matching OAuth transaction and local
// origin. Save must validate the token before persisting it and must not log it.
func Handler(state string, save func(string) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/twitch/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Receipts authorization</title></head><body><h1>Receipts</h1><p id="status">Completing Twitch authorization…</p><script src="/callback.js"></script></body></html>`))
	})
	mux.HandleFunc("GET /callback.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write([]byte(`(async () => {
 const params = new URLSearchParams(location.hash.slice(1));
 history.replaceState(null, '', '/auth/twitch/callback');
 const status = document.getElementById('status');
 if (!params.get('access_token') || !params.get('state')) {
   status.textContent = 'Authorization was declined or incomplete. No credentials saved.'; return;
 }
 try {
   const response = await fetch('/token', {method:'POST', headers:{'Content-Type':'application/json'},
     body: JSON.stringify({token:params.get('access_token'), state:params.get('state')})});
   status.textContent = response.ok ? 'Authorized. Probe credentials saved locally. You can close this tab.' : 'Authorization could not be verified. No credentials saved.';
 } catch { status.textContent = 'Local receiver unavailable. No credentials saved.'; }
})();`))
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != Origin || r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "refused", http.StatusForbidden)
			return
		}
		var input struct {
			Token string `json:"token"`
			State string `json:"state"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || input.Token == "" || state == "" || subtle.ConstantTimeCompare([]byte(input.State), []byte(state)) != 1 {
			http.Error(w, "invalid authorization", http.StatusBadRequest)
			return
		}
		if save(input.Token) != nil {
			http.Error(w, "authorization could not be saved", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Host != "localhost:18081" {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

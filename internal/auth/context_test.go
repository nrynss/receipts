package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ownerContext(f *authFixture, session *http.Cookie, account Account) ChannelContext {
	sum := sha256.Sum256([]byte(session.Value))
	return ChannelContext{AccountID: account.ID, ChannelID: account.ChannelID, BrowserSessionID: hex.EncodeToString(sum[:])}
}

func TestDeviceCredentialHashReadOnlyAndImmediateRevocation(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "devices.db"), nil)
	session, _, account := login(t, f.service)
	owner := ownerContext(f, session, account)
	deviceID, credential, err := f.service.IssueDeviceCredential(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	foreign := owner
	foreign.ChannelID = "guessed-channel"
	if _, _, err := f.service.IssueDeviceCredential(context.Background(), foreign); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("device issued for mismatched owner/channel: %v", err)
	}
	var stored []byte
	if err := f.db.Reader().QueryRowContext(context.Background(), "SELECT token_hash FROM device_sessions WHERE id=?", deviceID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(credential))
	if string(stored) != string(want[:]) {
		t.Fatal("device secret was not stored as a digest")
	}

	entered, canceled := make(chan struct{}), make(chan struct{})
	handler := f.service.ChannelMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value, err := ChannelFromContext(r.Context()); err != nil || !value.Device || value.DeviceID != deviceID || value.ChannelID != account.ChannelID {
			t.Errorf("device context=%+v err=%v", value, err)
		}
		close(entered)
		<-r.Context().Done()
		close(canceled)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+credential)
	go handler.ServeHTTP(httptest.NewRecorder(), req)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("device stream did not start")
	}
	control := httptest.NewRequest(http.MethodPost, "/api/control/pause", strings.NewReader("{\"paused\":true}"))
	control.Header.Set("Authorization", "Bearer "+credential)
	blocked := httptest.NewRecorder()
	f.service.ChannelMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("device reached control handler") })).ServeHTTP(blocked, control)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("device control status=%d", blocked.Code)
	}
	if err := f.service.RevokeDeviceCredential(context.Background(), owner, deviceID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("device revocation did not cancel active stream")
	}

	for _, path := range []string{"/api/overlay", "/api/events"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+credential)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("revoked device status=%d", res.Code)
		}
	}
}

func TestLogoutCancelsOwnerSSEImmediately(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "session-stream.db"), nil)
	session, csrf, _ := login(t, f.service)
	entered, canceled := make(chan struct{}), make(chan struct{})
	handler := f.service.ChannelMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(canceled)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.AddCookie(session)
	go handler.ServeHTTP(httptest.NewRecorder(), req)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("owner stream did not start")
	}
	logout := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logout.AddCookie(session)
	logout.AddCookie(csrf)
	logout.Header.Set("X-CSRF-Token", csrf.Value)
	logout.Header.Set("Origin", "https://receipts.example")
	res := httptest.NewRecorder()
	f.service.Handler().ServeHTTP(res, logout)
	if res.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d %s", res.Code, res.Body.String())
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("logout did not cancel active SSE")
	}
}

func TestOwnerSSEExpiresWithBrowserSession(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "session-expiry.db"), nil)
	session, _, account := login(t, f.service)
	if _, err := f.db.Writer().ExecContext(context.Background(), "UPDATE browser_sessions SET expires_at=? WHERE token_hash=?", f.clock.Now().Add(60*time.Millisecond).UnixMilli(), func() []byte { h := sha256.Sum256([]byte(session.Value)); return h[:] }()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	handler := f.service.ChannelMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done(); close(done) }))
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.AddCookie(session)
	go handler.ServeHTTP(httptest.NewRecorder(), req)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("SSE for %s did not expire", account.ID)
	}
}

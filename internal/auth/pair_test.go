package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPairBrowserBoundSingleUseAndCSRF(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "pair.db"), nil)
	session, csrf, account := login(t, f.service)
	start := httptest.NewRequest(http.MethodPost, "https://receipts.example/auth/pair/start", nil)
	start.Header.Set("Origin", "https://receipts.example")
	out := httptest.NewRecorder()
	f.service.Handler().ServeHTTP(out, start)
	if out.Code != 200 {
		t.Fatalf("start: %d", out.Code)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	pairCookie := out.Result().Cookies()[0]
	confirm := func(withCSRF bool) int {
		req := httptest.NewRequest(http.MethodPost, "https://receipts.example/auth/pair/confirm", strings.NewReader(`{"code":"`+body.Code+`"}`))
		req.AddCookie(session)
		req.AddCookie(csrf)
		req.Header.Set("Origin", "https://receipts.example")
		if withCSRF {
			req.Header.Set("X-CSRF-Token", csrf.Value)
		}
		res := httptest.NewRecorder()
		f.service.Handler().ServeHTTP(res, req)
		return res.Code
	}
	if code := confirm(false); code != 403 {
		t.Fatalf("CSRF: %d", code)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- confirm(true) }()
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for c := range codes {
		counts[c]++
	}
	if counts[204] != 1 || counts[404] != 1 {
		t.Fatalf("concurrent: %v", counts)
	}
	status := httptest.NewRequest(http.MethodGet, "https://receipts.example/auth/pair/status", nil)
	status.AddCookie(pairCookie)
	res := httptest.NewRecorder()
	f.service.Handler().ServeHTTP(res, status)
	if res.Code != 204 {
		t.Fatalf("status: %d", res.Code)
	}
	var device *http.Cookie
	for _, c := range res.Result().Cookies() {
		if c.Name == DeviceCookie {
			device = c
		}
	}
	if device == nil || !device.Secure || !device.HttpOnly {
		t.Fatal("device cookie missing or insecure")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/overlay", nil)
	req.AddCookie(device)
	f.service.ChannelMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		identity, err := ChannelFromContext(r.Context())
		if err != nil || identity.ChannelID != account.ChannelID || !identity.Device {
			t.Errorf("identity: %+v %v", identity, err)
		}
	})).ServeHTTP(httptest.NewRecorder(), req)
	again := httptest.NewRecorder()
	f.service.Handler().ServeHTTP(again, status)
	if again.Code != 410 {
		t.Fatalf("reused: %d", again.Code)
	}
}

func TestPairExpiryAndDeviceListScope(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "expiry.db"), nil)
	session, csrf, _ := login(t, f.service)
	start := httptest.NewRequest(http.MethodPost, "https://receipts.example/auth/pair/start", nil)
	start.Header.Set("Origin", "https://receipts.example")
	out := httptest.NewRecorder()
	f.service.Handler().ServeHTTP(out, start)
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(pairLifetime)
	req := httptest.NewRequest(http.MethodPost, "https://receipts.example/auth/pair/confirm", strings.NewReader(`{"code":"`+body.Code+`"}`))
	req.AddCookie(session)
	req.AddCookie(csrf)
	req.Header.Set("Origin", "https://receipts.example")
	req.Header.Set("X-CSRF-Token", csrf.Value)
	res := httptest.NewRecorder()
	f.service.Handler().ServeHTTP(res, req)
	if res.Code != 404 {
		t.Fatalf("expired: %d", res.Code)
	}
}

func TestDeviceManagementDoesNotCrossAccounts(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "owners.db"), nil)
	first, _, a := login(t, f.service)
	id, _, err := f.service.IssueDeviceCredential(context.Background(), ownerContext(f, first, a))
	if err != nil {
		t.Fatal(err)
	}
	f.provider.mu.Lock()
	f.provider.grant.UserID = "owner-2"
	f.provider.grant.Login = "other"
	f.provider.mu.Unlock()
	second, csrf, _ := login(t, f.service)
	req := httptest.NewRequest(http.MethodGet, "https://receipts.example/auth/devices", nil)
	req.AddCookie(second)
	out := httptest.NewRecorder()
	f.service.Handler().ServeHTTP(out, req)
	if strings.Contains(out.Body.String(), id) {
		t.Fatal("foreign device listed")
	}
	revoke := httptest.NewRequest(http.MethodPost, "https://receipts.example/auth/devices/"+id+"/revoke", nil)
	revoke.AddCookie(second)
	revoke.AddCookie(csrf)
	revoke.Header.Set("Origin", "https://receipts.example")
	revoke.Header.Set("X-CSRF-Token", csrf.Value)
	out = httptest.NewRecorder()
	f.service.Handler().ServeHTTP(out, revoke)
	if out.Code != 404 {
		t.Fatalf("foreign revoke: %d", out.Code)
	}
}

func TestDeviceCookieSurvivesServiceRestart(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "restart.db"), nil)
	session, _, a := login(t, f.service)
	_, credential, err := f.service.IssueDeviceCredential(context.Background(), ownerContext(f, session, a))
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(context.Background(), Config{DB: f.db, Provider: f.provider, ClientID: "app-1", RedirectURL: "https://receipts.example/auth/twitch/callback", PublicOrigin: "https://receipts.example", EncryptionKey: bytes32('k'), SecureCookie: true, Now: f.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "https://receipts.example/api/overlay", nil)
	req.AddCookie(&http.Cookie{Name: DeviceCookie, Value: credential})
	seen := false
	restarted.ChannelMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		identity, err := ChannelFromContext(r.Context())
		seen = err == nil && identity.Device && identity.ChannelID == a.ChannelID
	})).ServeHTTP(httptest.NewRecorder(), req)
	if !seen {
		t.Fatal("device unavailable after restart")
	}
}

func TestRevokedBeforePollCannotReportPaired(t *testing.T) {
	testRevokedBeforePoll(t, false)
}

func TestDisconnectedBeforePollCannotReportPaired(t *testing.T) {
	testRevokedBeforePoll(t, true)
}

func testRevokedBeforePoll(t *testing.T, disconnect bool) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "poll-revoke.db"), nil)
	session, csrf, a := login(t, f.service)
	start := httptest.NewRequest(http.MethodPost, "https://receipts.example/auth/pair/start", nil)
	start.Header.Set("Origin", "https://receipts.example")
	out := httptest.NewRecorder()
	f.service.Handler().ServeHTTP(out, start)
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	browser := findCookie(t, out.Result().Cookies(), pairCookie)
	confirm := httptest.NewRequest(http.MethodPost, "https://receipts.example/auth/pair/confirm", strings.NewReader(`{"code":"`+body.Code+`"}`))
	confirm.AddCookie(session)
	confirm.AddCookie(csrf)
	confirm.Header.Set("Origin", "https://receipts.example")
	confirm.Header.Set("X-CSRF-Token", csrf.Value)
	out = httptest.NewRecorder()
	f.service.Handler().ServeHTTP(out, confirm)
	if out.Code != 204 {
		t.Fatalf("confirm %d", out.Code)
	}
	var id string
	if err := f.db.Reader().QueryRowContext(context.Background(), `SELECT id FROM device_sessions WHERE account_id=?`, a.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if disconnect {
		if err := f.service.RevokeProviderAuthorization(context.Background(), a.ID); err != nil {
			t.Fatal(err)
		}
	} else if err := f.service.RevokeDeviceCredential(context.Background(), ownerContext(f, session, a), id); err != nil {
		t.Fatal(err)
	}
	poll := httptest.NewRequest(http.MethodGet, "https://receipts.example/auth/pair/status", nil)
	poll.AddCookie(browser)
	out = httptest.NewRecorder()
	f.service.Handler().ServeHTTP(out, poll)
	if out.Code != http.StatusGone {
		t.Fatalf("revoked poll %d", out.Code)
	}
	for _, c := range out.Result().Cookies() {
		if c.Name == DeviceCookie && c.Value != "" {
			t.Fatal("issued revoked cookie")
		}
	}
}

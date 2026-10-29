package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/store"
)

type authClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *authClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *authClock) Add(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

type fakeProvider struct {
	mu            sync.Mutex
	grant         Grant
	tokens        Tokens
	refreshTokens Tokens
	refreshCalls  int
	refreshErr    error
	refreshWait   time.Duration
	validateErr   error
	exchangeCalls int
}

func (p *fakeProvider) AuthorizeURL(state, redirect string, scopes []string) string {
	u, _ := url.Parse("https://provider.test/authorize")
	q := u.Query()
	q.Set("state", state)
	q.Set("redirect_uri", redirect)
	u.RawQuery = q.Encode()
	return u.String()
}
func (p *fakeProvider) ExchangeCode(context.Context, string, string) (Tokens, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.exchangeCalls++
	return p.tokens, nil
}
func (p *fakeProvider) Validate(context.Context, string) (Grant, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.grant, p.validateErr
}
func (p *fakeProvider) Refresh(context.Context, string) (Tokens, error) {
	p.mu.Lock()
	p.refreshCalls++
	delay, tokens, err := p.refreshWait, p.refreshTokens, p.refreshErr
	p.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	return tokens, err
}

type authFixture struct {
	path     string
	db       *sqlite.DB
	service  *Service
	provider *fakeProvider
	clock    *authClock
}

func newAuthFixture(t *testing.T, path string, clock *authClock) *authFixture {
	t.Helper()
	if clock == nil {
		clock = &authClock{now: time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)}
	}
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: path, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.New(context.Background(), db, store.Retention{}, clock.Now); err != nil {
		t.Fatal(err)
	}
	p := &fakeProvider{tokens: Tokens{AccessToken: "access-secret", RefreshToken: "refresh-secret", ExpiresIn: time.Hour}, grant: Grant{ClientID: "app-1", UserID: "owner-1", Login: "owner", Scopes: append([]string(nil), DefaultScopes...), Expires: time.Hour}}
	svc, err := New(context.Background(), Config{DB: db, Provider: p, ClientID: "app-1", RedirectURL: "https://receipts.example/auth/twitch/callback", PublicOrigin: "https://receipts.example", EncryptionKey: bytes32('k'), SecureCookie: true, Now: clock.Now})
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &authFixture{path: path, db: db, service: svc, provider: p, clock: clock}
}
func bytes32(b byte) []byte {
	v := make([]byte, 32)
	for i := range v {
		v[i] = b
	}
	return v
}

func login(t *testing.T, svc *Service, previous ...*http.Cookie) (*http.Cookie, *http.Cookie, Account) {
	t.Helper()
	handler := svc.Handler()
	start := httptest.NewRecorder()
	handler.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/twitch", nil))
	if start.Code != http.StatusFound {
		t.Fatalf("start status=%d", start.Code)
	}
	stateCookie := findCookie(t, start.Result().Cookies(), oauthBrowserCookie)
	loc, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("authorization URL omitted state")
	}
	callback := httptest.NewRequest(http.MethodGet, "/auth/twitch/callback?state="+url.QueryEscape(state)+"&code=one-time-code", nil)
	callback.AddCookie(stateCookie)
	if len(previous) > 0 && previous[0] != nil {
		callback.AddCookie(previous[0])
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, callback)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("callback status=%d body=%q", response.Code, response.Body.String())
	}
	session := findCookie(t, response.Result().Cookies(), SessionCookie)
	csrf := findCookie(t, response.Result().Cookies(), CSRFCookie)
	if !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode || csrf.HttpOnly || !csrf.Secure {
		t.Fatalf("cookie flags: session=%#v csrf=%#v", session, csrf)
	}
	r := httptest.NewRequest(http.MethodGet, "/private", nil)
	r.AddCookie(session)
	a, err := svc.Current(r)
	if err != nil {
		t.Fatal(err)
	}
	return session, csrf, a
}
func findCookie(t *testing.T, all []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, c := range all {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("cookie %q not found", name)
	return nil
}

func TestLoginRepeatsSameAccountAndChannelWithRotatedSession(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "auth.db"), nil)
	first, _, a1 := login(t, f.service)
	second, _, a2 := login(t, f.service, first)
	if a1.ID != a2.ID || a1.ChannelID != a2.ChannelID || a1.TwitchID != "owner-1" {
		t.Fatalf("accounts changed: %#v %#v", a1, a2)
	}
	if first.Value == second.Value {
		t.Fatal("login did not rotate browser session")
	}
	r := httptest.NewRequest(http.MethodGet, "/private", nil)
	r.AddCookie(first)
	if _, err := f.service.Current(r); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("prior session remains active: %v", err)
	}
	var accounts, channels int
	if err := f.db.Reader().QueryRow(`SELECT count(*) FROM accounts`).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Reader().QueryRow(`SELECT count(*) FROM channels`).Scan(&channels); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 || channels != 1 {
		t.Fatalf("duplicate owner rows: accounts=%d channels=%d", accounts, channels)
	}
}

func TestCallbackRejectsStateAppIdentityAndMissingGrants(t *testing.T) {
	tests := []struct {
		name  string
		alter func(*fakeProvider)
	}{
		{"wrong app", func(p *fakeProvider) { p.grant.ClientID = "other-app" }},
		{"missing grant", func(p *fakeProvider) { p.grant.Scopes = []string{"user:read:chat"} }},
		{"missing identity", func(p *fakeProvider) { p.grant.UserID = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuthFixture(t, filepath.Join(t.TempDir(), "auth.db"), nil)
			tc.alter(f.provider)
			start := httptest.NewRecorder()
			f.service.Handler().ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/twitch", nil))
			stateCookie := findCookie(t, start.Result().Cookies(), oauthBrowserCookie)
			loc, _ := url.Parse(start.Header().Get("Location"))
			cb := httptest.NewRequest(http.MethodGet, "/auth/twitch/callback?state="+url.QueryEscape(loc.Query().Get("state"))+"&code=secret-code", nil)
			cb.AddCookie(stateCookie)
			out := httptest.NewRecorder()
			f.service.Handler().ServeHTTP(out, cb)
			if out.Code != http.StatusForbidden || strings.Contains(out.Body.String(), "secret-code") || len(out.Result().Cookies()) > 1 {
				t.Fatalf("unsafe callback result: %d %q %#v", out.Code, out.Body.String(), out.Result().Cookies())
			}
			var n int
			if err := f.db.Reader().QueryRow(`SELECT count(*) FROM accounts`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("account created after invalid grant: %d %v", n, err)
			}
		})
	}
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "state.db"), nil)
	start := httptest.NewRecorder()
	f.service.Handler().ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/twitch", nil))
	stateCookie := findCookie(t, start.Result().Cookies(), oauthBrowserCookie)
	loc, _ := url.Parse(start.Header().Get("Location"))
	state := loc.Query().Get("state")
	cb := httptest.NewRequest(http.MethodGet, "/auth/twitch/callback?state=attacker&code=secret-code", nil)
	cb.AddCookie(stateCookie)
	out := httptest.NewRecorder()
	f.service.Handler().ServeHTTP(out, cb)
	if out.Code != http.StatusBadRequest || f.provider.exchangeCalls != 0 {
		t.Fatalf("state mismatch reached provider: %d calls=%d", out.Code, f.provider.exchangeCalls)
	}
	replay := httptest.NewRequest(http.MethodGet, "/auth/twitch/callback?state="+url.QueryEscape(state)+"&code=secret-code", nil)
	replay.AddCookie(stateCookie)
	replayed := httptest.NewRecorder()
	f.service.Handler().ServeHTTP(replayed, replay)
	if replayed.Code != http.StatusSeeOther {
		t.Fatalf("valid state after mismatch was not usable: %d", replayed.Code)
	}
	repeated := httptest.NewRequest(http.MethodGet, "/auth/twitch/callback?state="+url.QueryEscape(state)+"&code=secret-code", nil)
	repeated.AddCookie(stateCookie)
	denied := httptest.NewRecorder()
	f.service.Handler().ServeHTTP(denied, repeated)
	if denied.Code != http.StatusBadRequest || f.provider.exchangeCalls != 1 {
		t.Fatalf("state replay reached provider: %d calls=%d", denied.Code, f.provider.exchangeCalls)
	}
	bound := newAuthFixture(t, filepath.Join(t.TempDir(), "browser-bound.db"), nil)
	start = httptest.NewRecorder()
	bound.service.Handler().ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/twitch", nil))
	stateCookie = findCookie(t, start.Result().Cookies(), oauthBrowserCookie)
	loc, _ = url.Parse(start.Header().Get("Location"))
	foreignBrowser := httptest.NewRequest(http.MethodGet, "/auth/twitch/callback?state="+url.QueryEscape(loc.Query().Get("state"))+"&code=secret-code", nil)
	foreignBrowser.AddCookie(&http.Cookie{Name: stateCookie.Name, Value: "different-browser-binding"})
	refused := httptest.NewRecorder()
	bound.service.Handler().ServeHTTP(refused, foreignBrowser)
	if refused.Code != http.StatusBadRequest || bound.provider.exchangeCalls != 0 {
		t.Fatalf("state was not bound to its browser: %d calls=%d", refused.Code, bound.provider.exchangeCalls)
	}
}

func TestCSRFSessionExpiryAndLogout(t *testing.T) {
	clock := &authClock{now: time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)}
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "auth.db"), clock)
	session, csrf, _ := login(t, f.service)
	handler := f.service.Handler()
	bad := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	bad.AddCookie(session)
	bad.Header.Set("Origin", "https://receipts.example")
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, bad)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF returned %d", denied.Code)
	}
	good := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	good.AddCookie(session)
	good.AddCookie(csrf)
	good.Header.Set("Origin", "https://receipts.example")
	good.Header.Set("X-CSRF-Token", csrf.Value)
	loggedOut := httptest.NewRecorder()
	handler.ServeHTTP(loggedOut, good)
	if loggedOut.Code != http.StatusNoContent {
		t.Fatalf("valid logout status %d", loggedOut.Code)
	}
	r := httptest.NewRequest(http.MethodGet, "/private", nil)
	r.AddCookie(session)
	if _, err := f.service.Current(r); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("logout left session active: %v", err)
	}
	expiredSession, _, _ := login(t, f.service)
	clock.Add(31 * 24 * time.Hour)
	expired := httptest.NewRequest(http.MethodGet, "/private", nil)
	expired.AddCookie(expiredSession)
	if _, err := f.service.Current(expired); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired session accepted: %v", err)
	}
}

func TestConcurrentRefreshRotatesOnceAndSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.db")
	clock := &authClock{now: time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)}
	f := newAuthFixture(t, path, clock)
	session, _, account := login(t, f.service)
	f.clock.Add(2 * time.Hour)
	f.provider.refreshTokens = Tokens{AccessToken: "rotated-access", RefreshToken: "rotated-refresh", ExpiresIn: time.Hour}
	f.provider.refreshWait = 10 * time.Millisecond
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := f.service.AccessToken(context.Background(), account.ID)
			if err == nil && got != "rotated-access" {
				err = errors.New("wrong access token returned")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	f.provider.mu.Lock()
	calls := f.provider.refreshCalls
	f.provider.mu.Unlock()
	if calls != 1 {
		t.Fatalf("refresh calls=%d, want 1", calls)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: path, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = store.New(context.Background(), db, store.Retention{}, clock.Now); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(context.Background(), Config{DB: db, Provider: f.provider, ClientID: "app-1", RedirectURL: "https://receipts.example/auth/twitch/callback", PublicOrigin: "https://receipts.example", EncryptionKey: bytes32('k'), SecureCookie: true, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.AccessToken(context.Background(), account.ID)
	if err != nil || got != "rotated-access" {
		t.Fatalf("restart token=%q err=%v", got, err)
	}
	r := httptest.NewRequest(http.MethodGet, "/private", nil)
	r.AddCookie(session)
	if _, err := restarted.Current(r); err != nil {
		t.Fatalf("restart lost browser session: %v", err)
	}
	dbBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"access-secret", "refresh-secret", "rotated-access", "rotated-refresh"} {
		if strings.Contains(string(dbBytes), secret) {
			t.Fatalf("database contains plaintext token %q", secret)
		}
	}
}

func TestRefreshRevocationRequiresReconnectAndErrorsAreRedacted(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "auth.db"), nil)
	_, _, a := login(t, f.service)
	f.clock.Add(2 * time.Hour)
	f.provider.refreshErr = ErrAuthorizationLost
	if _, err := f.service.AccessToken(context.Background(), a.ID); !errors.Is(err, ErrReconnect) {
		t.Fatalf("refresh error=%v", err)
	}
	var status string
	if err := f.db.Reader().QueryRow(`SELECT status FROM accounts WHERE id=?`, a.ID).Scan(&status); err != nil || status != "reconnect_required" {
		t.Fatalf("status=%q err=%v", status, err)
	}
	if strings.Contains(ErrReconnect.Error(), "refresh-secret") {
		t.Fatal("reconnect error exposed token")
	}
}

func TestRefreshRejectsChangedIdentity(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "auth.db"), nil)
	_, _, a := login(t, f.service)
	f.clock.Add(2 * time.Hour)
	f.provider.refreshTokens = Tokens{AccessToken: "rotated-access", RefreshToken: "rotated-refresh", ExpiresIn: time.Hour}
	f.provider.mu.Lock()
	f.provider.grant.UserID = "attacker"
	f.provider.mu.Unlock()
	if _, err := f.service.AccessToken(context.Background(), a.ID); !errors.Is(err, ErrReconnect) {
		t.Fatalf("changed identity error=%v", err)
	}
	var status string
	if err := f.db.Reader().QueryRow(`SELECT status FROM accounts WHERE id=?`, a.ID).Scan(&status); err != nil || status != "reconnect_required" {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

func TestUnauthorizedRefreshSkipsAStaleRejectionAndRotatesCurrentToken(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "auth.db"), nil)
	_, _, account := login(t, f.service)
	f.provider.refreshTokens = Tokens{AccessToken: "rotated-access", RefreshToken: "rotated-refresh", ExpiresIn: time.Hour}
	got, err := f.service.RefreshAfterUnauthorized(context.Background(), account.ID, "rejected-old-token")
	if err != nil || got != "access-secret" {
		t.Fatalf("stale unauthorized token result=%q err=%v", got, err)
	}
	got, err = f.service.RefreshAfterUnauthorized(context.Background(), account.ID, "access-secret")
	if err != nil || got != "rotated-access" {
		t.Fatalf("current unauthorized token result=%q err=%v", got, err)
	}
	got, err = f.service.RefreshAfterUnauthorized(context.Background(), account.ID, "access-secret")
	if err != nil || got != "rotated-access" {
		t.Fatalf("replayed rejection result=%q err=%v", got, err)
	}
	f.provider.mu.Lock()
	calls := f.provider.refreshCalls
	f.provider.mu.Unlock()
	if calls != 1 {
		t.Fatalf("refresh calls=%d, want 1", calls)
	}
}

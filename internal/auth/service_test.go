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
	mu             sync.Mutex
	grant          Grant
	tokens         Tokens
	refreshTokens  Tokens
	refreshCalls   int
	refreshErr     error
	refreshWait    time.Duration
	validateErr    error
	exchangeCalls  int
	validateCalls  int
	onRefresh      func(context.Context)
	refreshStarted chan struct{}
	refreshRelease chan struct{}
	validateNotify chan struct{}
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
func (p *fakeProvider) Validate(ctx context.Context, _ string) (Grant, error) {
	p.mu.Lock()
	p.validateCalls++
	grant, err, notify := p.grant, p.validateErr, p.validateNotify
	p.mu.Unlock()
	if notify != nil {
		select {
		case notify <- struct{}{}:
		default:
		}
	}
	if ctx.Err() != nil {
		return Grant{}, ctx.Err()
	}
	return grant, err
}
func (p *fakeProvider) Refresh(ctx context.Context, _ string) (Tokens, error) {
	p.mu.Lock()
	p.refreshCalls++
	delay, tokens, err := p.refreshWait, p.refreshTokens, p.refreshErr
	started, release, onRefresh := p.refreshStarted, p.refreshRelease, p.onRefresh
	p.mu.Unlock()
	if started != nil {
		close(started)
	}
	if release != nil {
		<-release
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	if onRefresh != nil {
		onRefresh(ctx)
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
	session, _, a := login(t, f.service)
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
	r := httptest.NewRequest(http.MethodGet, "/private", nil)
	r.AddCookie(session)
	if _, err := f.service.Current(r); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked provider grant left browser session active: %v", err)
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

func TestLoginUpsertSerializesWithRefresh(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "auth.db"), nil)
	_, _, account := login(t, f.service)
	f.clock.Add(2 * time.Hour)
	started, release := make(chan struct{}), make(chan struct{})
	f.provider.mu.Lock()
	f.provider.refreshTokens = Tokens{AccessToken: "rotated-before-login", RefreshToken: "refresh-before-login", ExpiresIn: time.Hour}
	f.provider.refreshStarted, f.provider.refreshRelease = started, release
	f.provider.mu.Unlock()
	refreshDone := make(chan error, 1)
	go func() {
		_, err := f.service.AccessToken(context.Background(), account.ID)
		refreshDone <- err
	}()
	<-started
	newTokens, err := f.service.seal(account.TwitchID, storedTokens{Access: "new-login-access", Refresh: "new-login-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	upsertDone := make(chan error, 1)
	go func() {
		_, err := f.service.upsertAccount(context.Background(), Grant{ClientID: "app-1", UserID: account.TwitchID, Login: "owner", Scopes: append([]string(nil), DefaultScopes...), Expires: time.Hour}, newTokens, time.Hour)
		upsertDone <- err
	}()
	select {
	case err := <-upsertDone:
		t.Fatalf("login upsert crossed in-flight refresh: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	if err := <-upsertDone; err != nil {
		t.Fatal(err)
	}
	f.provider.mu.Lock()
	f.provider.refreshStarted, f.provider.refreshRelease = nil, nil
	f.provider.mu.Unlock()
	got, err := f.service.AccessToken(context.Background(), account.ID)
	if err != nil || got != "new-login-access" {
		t.Fatalf("login credentials lost after refresh: %v", err)
	}
}

func TestProviderRevocationSerializesWithRefreshAndRevokesSessions(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "auth.db"), nil)
	session, _, account := login(t, f.service)
	f.clock.Add(2 * time.Hour)
	started, release := make(chan struct{}), make(chan struct{})
	f.provider.mu.Lock()
	f.provider.refreshTokens = Tokens{AccessToken: "rotated-access", RefreshToken: "rotated-refresh", ExpiresIn: time.Hour}
	f.provider.refreshStarted, f.provider.refreshRelease = started, release
	f.provider.mu.Unlock()
	refreshDone := make(chan error, 1)
	go func() { _, err := f.service.AccessToken(context.Background(), account.ID); refreshDone <- err }()
	<-started
	revokeDone := make(chan error, 1)
	go func() { revokeDone <- f.service.RevokeProviderAuthorization(context.Background(), account.ID) }()
	select {
	case err := <-revokeDone:
		t.Fatalf("revoke crossed in-flight refresh: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	if err := <-revokeDone; err != nil {
		t.Fatal(err)
	}
	f.provider.mu.Lock()
	f.provider.refreshStarted, f.provider.refreshRelease = nil, nil
	f.provider.mu.Unlock()
	var status string
	if err := f.db.Reader().QueryRow(`SELECT status FROM accounts WHERE id=?`, account.ID).Scan(&status); err != nil || status != "reconnect_required" {
		t.Fatalf("account status=%q err=%v", status, err)
	}
	r := httptest.NewRequest(http.MethodGet, "/private", nil)
	r.AddCookie(session)
	if _, err := f.service.Current(r); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("provider revocation left browser session active: %v", err)
	}
}

func TestSessionCannotBeIssuedAfterRevocationBetweenLoginSteps(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "auth.db"), nil)
	session, _, account := login(t, f.service)
	newTokens, err := f.service.seal(account.TwitchID, storedTokens{Access: "reconnected-access", Refresh: "reconnected-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.upsertAccount(context.Background(), Grant{ClientID: "app-1", UserID: account.TwitchID, Login: "owner", Scopes: append([]string(nil), DefaultScopes...), Expires: time.Hour}, newTokens, time.Hour); err != nil {
		t.Fatal(err)
	}
	// This is the callback interleaving between account upsert and session
	// issuance: revocation must prevent the callback from minting a fresh row.
	if err := f.service.RevokeProviderAuthorization(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.service.issueSession(context.Background(), account.ID, session.Value); !errors.Is(err, ErrReconnect) {
		t.Fatalf("session issued after provider revocation: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/private", nil)
	r.AddCookie(session)
	if _, err := f.service.Current(r); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old session survived callback/revoke interleaving: %v", err)
	}
	var active int
	if err := f.db.Reader().QueryRow(`SELECT count(*) FROM browser_sessions WHERE account_id=? AND revoked_at IS NULL`, account.ID).Scan(&active); err != nil || active != 0 {
		t.Fatalf("active sessions=%d err=%v", active, err)
	}
}

func TestSuccessfulRefreshPersistsWhenCallerCancelsAndValidationTemporarilyFails(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "auth.db"), nil)
	_, _, account := login(t, f.service)
	f.clock.Add(2 * time.Hour)
	f.provider.refreshTokens = Tokens{AccessToken: "rotated-access", RefreshToken: "rotated-refresh", ExpiresIn: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	f.provider.onRefresh = func(context.Context) { cancel() }
	got, err := f.service.AccessToken(ctx, account.ID)
	if err != nil || got != "rotated-access" {
		t.Fatalf("caller cancellation lost rotation: %v", err)
	}
	f.provider.onRefresh = nil
	if got, err = f.service.AccessToken(context.Background(), account.ID); err != nil || got != "rotated-access" {
		t.Fatalf("persisted rotation was not reused: %v", err)
	}
	f.provider.mu.Lock()
	calls := f.provider.refreshCalls
	f.provider.mu.Unlock()
	if calls != 1 {
		t.Fatalf("refresh calls after cancellation=%d, want 1", calls)
	}

	g := newAuthFixture(t, filepath.Join(t.TempDir(), "validation-outage.db"), nil)
	_, _, nextAccount := login(t, g.service)
	g.clock.Add(2 * time.Hour)
	g.provider.refreshTokens = Tokens{AccessToken: "outage-rotated-access", RefreshToken: "outage-rotated-refresh", ExpiresIn: time.Hour}
	g.provider.mu.Lock()
	g.provider.validateErr = errors.New("temporary provider outage")
	g.provider.mu.Unlock()
	if _, err := g.service.AccessToken(context.Background(), nextAccount.ID); err == nil {
		t.Fatal("temporary validation outage was accepted")
	}
	var pending int
	if err := g.db.Reader().QueryRow(`SELECT token_validation_pending FROM accounts WHERE id=?`, nextAccount.ID).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("rotated pair pending=%d err=%v", pending, err)
	}
	g.provider.mu.Lock()
	g.provider.validateErr = nil
	refreshCalls := g.provider.refreshCalls
	g.provider.mu.Unlock()
	got, err = g.service.AccessToken(context.Background(), nextAccount.ID)
	if err != nil || got != "outage-rotated-access" {
		t.Fatalf("pending token did not recover: %v", err)
	}
	g.provider.mu.Lock()
	defer g.provider.mu.Unlock()
	if g.provider.refreshCalls != refreshCalls {
		t.Fatalf("validation recovery spent another refresh token: calls=%d before=%d", g.provider.refreshCalls, refreshCalls)
	}
}

func TestStartupHourlyAndIdleMaintenanceValidation(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "auth.db"), nil)
	f.provider.tokens.ExpiresIn = 4 * time.Hour
	f.provider.grant.Expires = 4 * time.Hour
	_, _, account := login(t, f.service)
	f.provider.mu.Lock()
	loginValidations := f.provider.validateCalls
	f.provider.mu.Unlock()
	if loginValidations != 1 {
		t.Fatalf("login validations=%d, want 1", loginValidations)
	}
	// Constructing a second service over the durable account is the restart
	// boundary; New must validate before it returns.
	restarted, err := New(context.Background(), Config{DB: f.db, Provider: f.provider, ClientID: "app-1", RedirectURL: "https://receipts.example/auth/twitch/callback", PublicOrigin: "https://receipts.example", EncryptionKey: bytes32('k'), SecureCookie: true, Now: f.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	f.provider.mu.Lock()
	startupValidations := f.provider.validateCalls
	f.provider.mu.Unlock()
	if startupValidations != loginValidations+1 {
		t.Fatalf("startup validations=%d, want %d", startupValidations, loginValidations+1)
	}
	f.clock.Add(59 * time.Minute)
	if _, err := restarted.AccessToken(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	f.provider.mu.Lock()
	beforeHour := f.provider.validateCalls
	f.provider.mu.Unlock()
	if beforeHour != startupValidations {
		t.Fatalf("early validation count=%d, want %d", beforeHour, startupValidations)
	}
	f.clock.Add(time.Minute)
	if _, err := restarted.AccessToken(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	f.provider.mu.Lock()
	afterHour := f.provider.validateCalls
	f.provider.mu.Unlock()
	if afterHour != beforeHour+1 {
		t.Fatalf("hourly validation count=%d, want %d", afterHour, beforeHour+1)
	}

	restarted.validationInterval = 5 * time.Millisecond
	notify := make(chan struct{}, 1)
	f.provider.mu.Lock()
	f.provider.validateNotify = notify
	f.provider.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { restarted.Maintain(ctx, nil); close(done) }()
	select {
	case <-notify:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("idle maintenance did not validate connected accounts")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not stop when its context was canceled")
	}
}

func TestTokenCiphertextCannotMoveBetweenAccounts(t *testing.T) {
	f := newAuthFixture(t, filepath.Join(t.TempDir(), "auth.db"), nil)
	_, _, first := login(t, f.service)
	f.provider.mu.Lock()
	f.provider.grant.UserID = "owner-2"
	f.provider.grant.Login = "owner_two"
	f.provider.mu.Unlock()
	_, _, second := login(t, f.service)
	var firstCipher, secondCipher []byte
	if err := f.db.Reader().QueryRow(`SELECT token_ciphertext FROM accounts WHERE id=?`, first.ID).Scan(&firstCipher); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Reader().QueryRow(`SELECT token_ciphertext FROM accounts WHERE id=?`, second.ID).Scan(&secondCipher); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Writer().Exec(`UPDATE accounts SET token_ciphertext=? WHERE id=?`, secondCipher, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Writer().Exec(`UPDATE accounts SET token_ciphertext=? WHERE id=?`, firstCipher, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AccessToken(context.Background(), first.ID); err == nil || err.Error() != "auth: credentials unavailable" {
		t.Fatalf("swapped ciphertext result=%v", err)
	}
}

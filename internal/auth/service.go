package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nrynss/keel/id"
	"github.com/nrynss/keel/sqlite"
	"receipts/internal/store"
)

const (
	SessionCookie      = "receipts_session"
	CSRFCookie         = "receipts_csrf"
	oauthBrowserCookie = "receipts_oauth_browser"
)

var (
	ErrUnauthorized = errors.New("auth: sign-in required")
	ErrForbidden    = errors.New("auth: request refused")
	ErrReconnect    = errors.New("auth: reconnect required")
)

var DefaultScopes = []string{"user:read:chat", "user:write:chat", "clips:edit"}

type Config struct {
	DB            *sqlite.DB
	Provider      Provider
	ClientID      string
	RedirectURL   string
	PublicOrigin  string
	EncryptionKey []byte
	Scopes        []string
	SessionTTL    time.Duration
	StateTTL      time.Duration
	SecureCookie  bool
	Now           func() time.Time
}

type Account struct {
	ID         string `json:"id"`
	TwitchID   string `json:"twitch_id"`
	Login      string `json:"login"`
	ChannelID  string `json:"channel_id"`
	Connection string `json:"connection"`
}

type storedTokens struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
}

type accountRow struct {
	Account
	Ciphertext []byte
	ExpiresAt  time.Time
	Scopes     []string
	Status     string
}

type Service struct {
	db         *sqlite.DB
	provider   Provider
	clientID   string
	redirect   string
	origin     string
	key        []byte
	scopes     []string
	sessionTTL time.Duration
	stateTTL   time.Duration
	secure     bool
	now        func() time.Time
	locksMu    sync.Mutex
	locks      map[string]*sync.Mutex
}

func New(ctx context.Context, cfg Config) (*Service, error) {
	if cfg.DB == nil || cfg.Provider == nil || strings.TrimSpace(cfg.ClientID) == "" || len(cfg.EncryptionKey) != 32 {
		return nil, errors.New("auth: database, provider, client ID, and 32-byte encryption key are required")
	}
	callback, err := url.Parse(cfg.RedirectURL)
	if err != nil || callback.Scheme == "" || callback.Host == "" || callback.User != nil || callback.RawQuery != "" || callback.Fragment != "" {
		return nil, errors.New("auth: callback URL must be an absolute URL")
	}
	origin, err := url.Parse(cfg.PublicOrigin)
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" && origin.Path != "/" {
		return nil, errors.New("auth: public origin must be an absolute origin")
	}
	if !cfg.SecureCookie || origin.Scheme != "https" {
		return nil, errors.New("auth: browser sessions require HTTPS and secure cookies")
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 30 * 24 * time.Hour
	}
	if cfg.StateTTL == 0 {
		cfg.StateTTL = 10 * time.Minute
	}
	if cfg.SessionTTL < time.Minute || cfg.SessionTTL > 365*24*time.Hour || cfg.StateTTL < time.Minute || cfg.StateTTL > time.Hour {
		return nil, errors.New("auth: invalid session or state lifetime")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = append([]string(nil), DefaultScopes...)
	}
	if err := validateScopeSet(cfg.Scopes, cfg.Scopes); err != nil {
		return nil, errors.New("auth: invalid required scopes")
	}
	if err := store.MigrateAccounts(ctx, cfg.DB); err != nil {
		return nil, fmt.Errorf("auth: migrate account storage: %w", err)
	}
	normalizedOrigin, err := canonicalOrigin(origin)
	if err != nil {
		return nil, errors.New("auth: public origin must be an HTTPS origin")
	}
	return &Service{db: cfg.DB, provider: cfg.Provider, clientID: cfg.ClientID, redirect: cfg.RedirectURL,
		origin: normalizedOrigin, key: append([]byte(nil), cfg.EncryptionKey...), scopes: append([]string(nil), cfg.Scopes...),
		sessionTTL: cfg.SessionTTL, stateTTL: cfg.StateTTL, secure: cfg.SecureCookie, now: cfg.Now, locks: map[string]*sync.Mutex{}}, nil
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/twitch", s.startLogin)
	mux.HandleFunc("GET /auth/twitch/callback", s.callback)
	mux.HandleFunc("POST /auth/logout", s.logout)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *Service) startLogin(w http.ResponseWriter, r *http.Request) {
	browser, err := randomToken()
	if err != nil {
		http.Error(w, "Sign-in unavailable", http.StatusInternalServerError)
		return
	}
	state, err := randomToken()
	if err != nil || s.saveState(r.Context(), state, browser) != nil {
		http.Error(w, "Sign-in unavailable", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, s.cookie(oauthBrowserCookie, browser, s.stateTTL, true))
	http.Redirect(w, r, s.provider.AuthorizeURL(state, s.redirect, s.scopes), http.StatusFound)
}

func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	states, codes := r.URL.Query()["state"], r.URL.Query()["code"]
	cookie, cookieErr := r.Cookie(oauthBrowserCookie)
	if len(states) != 1 || cookieErr != nil || len(codes) > 1 || len(r.URL.Query()["error"]) > 1 {
		authFailure(w, http.StatusBadRequest)
		return
	}
	if !s.consumeState(r.Context(), states[0], cookie.Value) {
		authFailure(w, http.StatusBadRequest)
		return
	}
	http.SetCookie(w, s.expiredCookie(oauthBrowserCookie, true))
	if len(r.URL.Query()["error"]) != 0 || len(codes) != 1 || codes[0] == "" {
		authFailure(w, http.StatusForbidden)
		return
	}
	tokens, err := s.provider.ExchangeCode(r.Context(), codes[0], s.redirect)
	if err != nil || tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.ExpiresIn <= 0 {
		authFailure(w, http.StatusBadGateway)
		return
	}
	grant, err := s.provider.Validate(r.Context(), tokens.AccessToken)
	if err != nil || grant.ClientID != s.clientID || grant.UserID == "" || !validLogin(grant.Login) || grant.Expires <= 0 || !hasScopes(grant.Scopes, s.scopes) {
		authFailure(w, http.StatusForbidden)
		return
	}
	expiresIn := tokens.ExpiresIn
	if grant.Expires < expiresIn {
		expiresIn = grant.Expires
	}
	sealed, err := s.seal(storedTokens{Access: tokens.AccessToken, Refresh: tokens.RefreshToken})
	if err != nil {
		authFailure(w, http.StatusInternalServerError)
		return
	}
	account, err := s.upsertAccount(r.Context(), grant, sealed, expiresIn)
	if err != nil {
		authFailure(w, http.StatusInternalServerError)
		return
	}
	old, _ := r.Cookie(SessionCookie)
	newSession, csrf, err := s.issueSession(r.Context(), account.ID, cookieValue(old))
	if err != nil {
		authFailure(w, http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, s.cookie(SessionCookie, newSession, s.sessionTTL, true))
	http.SetCookie(w, s.cookie(CSRFCookie, csrf, s.sessionTTL, false))
	http.Redirect(w, r, s.origin+"/", http.StatusSeeOther)
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	account, sessionHash, err := s.authenticate(r)
	if err != nil || account.ID == "" || !s.validCSRF(r, sessionHash) || !s.validOrigin(r) {
		http.Error(w, "Request refused", http.StatusForbidden)
		return
	}
	_, err = s.db.Writer().ExecContext(r.Context(), `UPDATE browser_sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, s.now().UTC().UnixMilli(), sessionHash)
	if err != nil {
		http.Error(w, "Sign-out unavailable", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, s.expiredCookie(SessionCookie, true))
	http.SetCookie(w, s.expiredCookie(CSRFCookie, false))
	w.WriteHeader(http.StatusNoContent)
}

// Current resolves a browser session on every request. The returned Account
// contains only local identifiers and a verified Twitch login.
func (s *Service) Current(r *http.Request) (Account, error) {
	a, _, err := s.authenticate(r)
	return a, err
}

func (s *Service) ValidateCSRF(r *http.Request) error {
	_, sessionHash, err := s.authenticate(r)
	if err != nil || !s.validOrigin(r) || !s.validCSRF(r, sessionHash) {
		return ErrForbidden
	}
	return nil
}

func (s *Service) authenticate(r *http.Request) (Account, []byte, error) {
	cookie, err := r.Cookie(SessionCookie)
	if err != nil || cookie.Value == "" {
		return Account{}, nil, ErrUnauthorized
	}
	hash := digest(cookie.Value)
	var a Account
	var expires int64
	var revoked sql.NullInt64
	err = s.db.Reader().QueryRowContext(r.Context(), `SELECT a.id,a.provider_subject,a.login,a.channel_id,a.status,b.expires_at,b.revoked_at FROM browser_sessions b JOIN accounts a ON a.id=b.account_id WHERE b.token_hash=?`, hash).Scan(&a.ID, &a.TwitchID, &a.Login, &a.ChannelID, &a.Connection, &expires, &revoked)
	if err != nil || revoked.Valid || expires <= s.now().UTC().UnixMilli() {
		return Account{}, nil, ErrUnauthorized
	}
	return a, hash, nil
}

func (s *Service) AccessToken(ctx context.Context, accountID string) (string, error) {
	lock := s.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	row, tokens, err := s.loadAccountTokens(ctx, accountID)
	if err != nil {
		return "", err
	}
	if row.Status != "connected" {
		return "", ErrReconnect
	}
	if row.ExpiresAt.After(s.now().UTC()) {
		return tokens.Access, nil
	}
	return s.refreshLocked(ctx, accountID, row, tokens)
}

// RefreshAfterUnauthorized refreshes a provider token after Helix rejects it.
// If another request already rotated the rejected token, it returns the fresh
// one without spending the rotated refresh token again.
func (s *Service) RefreshAfterUnauthorized(ctx context.Context, accountID, rejectedAccessToken string) (string, error) {
	if accountID == "" || rejectedAccessToken == "" {
		return "", ErrUnauthorized
	}
	lock := s.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	row, tokens, err := s.loadAccountTokens(ctx, accountID)
	if err != nil {
		return "", err
	}
	if row.Status != "connected" {
		return "", ErrReconnect
	}
	if tokens.Access != rejectedAccessToken {
		return tokens.Access, nil
	}
	return s.refreshLocked(ctx, accountID, row, tokens)
}

func (s *Service) refreshLocked(ctx context.Context, accountID string, row accountRow, tokens storedTokens) (string, error) {
	rotated, err := s.provider.Refresh(ctx, tokens.Refresh)
	if errors.Is(err, ErrAuthorizationLost) {
		_ = s.markReconnect(ctx, accountID)
		return "", ErrReconnect
	}
	if err != nil || rotated.AccessToken == "" || rotated.RefreshToken == "" || rotated.ExpiresIn <= 0 {
		return "", errors.New("auth: provider refresh failed")
	}
	grant, err := s.provider.Validate(ctx, rotated.AccessToken)
	if err != nil {
		if errors.Is(err, ErrAuthorizationLost) {
			_ = s.markReconnect(ctx, accountID)
			return "", ErrReconnect
		}
		return "", errors.New("auth: refreshed authorization could not be validated")
	}
	if grant.ClientID != s.clientID || grant.UserID != row.TwitchID || !validLogin(grant.Login) || grant.Expires <= 0 || !hasScopes(grant.Scopes, s.scopes) {
		_ = s.markReconnect(ctx, accountID)
		return "", ErrReconnect
	}
	expiresIn := rotated.ExpiresIn
	if grant.Expires < expiresIn {
		expiresIn = grant.Expires
	}
	sealed, err := s.seal(storedTokens{Access: rotated.AccessToken, Refresh: rotated.RefreshToken})
	if err != nil {
		return "", err
	}
	res, err := s.db.Writer().ExecContext(ctx, `UPDATE accounts SET token_ciphertext=?,token_expires_at=?,scopes=?,status='connected',updated_at=? WHERE id=? AND provider_subject=?`, sealed, s.now().UTC().Add(expiresIn).UnixMilli(), encodeScopes(grant.Scopes), s.now().UTC().UnixMilli(), accountID, row.TwitchID)
	if err != nil {
		return "", errors.New("auth: rotated credentials could not be saved")
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", ErrReconnect
	}
	return rotated.AccessToken, nil
}

// RevokeProviderAuthorization makes subsequent browser requests reconnect.
func (s *Service) RevokeProviderAuthorization(ctx context.Context, accountID string) error {
	return s.markReconnect(ctx, accountID)
}

func (s *Service) saveState(ctx context.Context, state, browser string) error {
	now := s.now().UTC()
	_, err := s.db.Writer().ExecContext(ctx, `DELETE FROM oauth_states WHERE expires_at<=?`, now.UnixMilli())
	if err != nil {
		return err
	}
	_, err = s.db.Writer().ExecContext(ctx, `INSERT INTO oauth_states(state_hash,browser_hash,created_at,expires_at) VALUES(?,?,?,?)`, digest(state), digest(browser), now.UnixMilli(), now.Add(s.stateTTL).UnixMilli())
	return err
}

func (s *Service) consumeState(ctx context.Context, state, browser string) bool {
	if state == "" || browser == "" {
		return false
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return false
	}
	defer tx.Rollback()
	var expected []byte
	var expires int64
	err = tx.QueryRowContext(ctx, `SELECT browser_hash,expires_at FROM oauth_states WHERE state_hash=?`, digest(state)).Scan(&expected, &expires)
	if err != nil || expires <= s.now().UTC().UnixMilli() || subtle.ConstantTimeCompare(expected, digest(browser)) != 1 {
		return false
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM oauth_states WHERE state_hash=? AND browser_hash=? AND expires_at>?`, digest(state), digest(browser), s.now().UTC().UnixMilli())
	if err != nil {
		return false
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return false
	}
	return tx.Commit() == nil
}

func (s *Service) upsertAccount(ctx context.Context, grant Grant, ciphertext []byte, expires time.Duration) (Account, error) {
	now := s.now().UTC()
	channelCandidate, err := id.New()
	if err != nil {
		return Account{}, err
	}
	accountCandidate, err := id.New()
	if err != nil {
		return Account{}, err
	}
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO channels(id,platform_id,login,created_at) VALUES(?,?,?,?) ON CONFLICT(platform_id) DO UPDATE SET login=excluded.login`, channelCandidate, grant.UserID, grant.Login, now.UnixMilli())
	if err != nil {
		return Account{}, err
	}
	var channelID string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM channels WHERE platform_id=?`, grant.UserID).Scan(&channelID); err != nil {
		return Account{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES(?,'twitch',?,?,?,?,?,?,'connected',?) ON CONFLICT(provider,provider_subject) DO UPDATE SET login=excluded.login,token_ciphertext=excluded.token_ciphertext,token_expires_at=excluded.token_expires_at,scopes=excluded.scopes,status='connected',updated_at=excluded.updated_at`, accountCandidate, grant.UserID, grant.Login, channelID, ciphertext, now.Add(expires).UnixMilli(), encodeScopes(grant.Scopes), now.UnixMilli())
	if err != nil {
		return Account{}, err
	}
	var a Account
	err = tx.QueryRowContext(ctx, `SELECT id,provider_subject,login,channel_id,status FROM accounts WHERE provider='twitch' AND provider_subject=?`, grant.UserID).Scan(&a.ID, &a.TwitchID, &a.Login, &a.ChannelID, &a.Connection)
	if err != nil {
		return Account{}, err
	}
	if err = tx.Commit(); err != nil {
		return Account{}, err
	}
	return a, nil
}

func (s *Service) issueSession(ctx context.Context, accountID, previous string) (string, string, error) {
	rawSession, err := randomToken()
	if err != nil {
		return "", "", err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", "", err
	}
	now := s.now().UTC()
	tx, err := s.db.BeginWrite(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	if previous != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE browser_sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, now.UnixMilli(), digest(previous)); err != nil {
			return "", "", err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO browser_sessions(token_hash,account_id,csrf_hash,created_at,expires_at) VALUES(?,?,?,?,?)`, digest(rawSession), accountID, digest(csrf), now.UnixMilli(), now.Add(s.sessionTTL).UnixMilli())
	if err != nil {
		return "", "", err
	}
	return rawSession, csrf, tx.Commit()
}

func (s *Service) loadAccountTokens(ctx context.Context, accountID string) (accountRow, storedTokens, error) {
	var row accountRow
	var expiry int64
	var scopes string
	err := s.db.Reader().QueryRowContext(ctx, `SELECT id,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status FROM accounts WHERE id=?`, accountID).Scan(&row.ID, &row.TwitchID, &row.Login, &row.ChannelID, &row.Ciphertext, &expiry, &scopes, &row.Status)
	if err != nil {
		return accountRow{}, storedTokens{}, ErrUnauthorized
	}
	row.ExpiresAt = time.UnixMilli(expiry).UTC()
	row.Scopes = decodeScopes(scopes)
	var tokens storedTokens
	if err = s.open(row.Ciphertext, &tokens); err != nil {
		return accountRow{}, storedTokens{}, errors.New("auth: credentials unavailable")
	}
	return row, tokens, nil
}

func (s *Service) markReconnect(ctx context.Context, accountID string) error {
	_, err := s.db.Writer().ExecContext(ctx, `UPDATE accounts SET status='reconnect_required',updated_at=? WHERE id=?`, s.now().UTC().UnixMilli(), accountID)
	return err
}

func (s *Service) accountLock(key string) *sync.Mutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	if lock := s.locks[key]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	s.locks[key] = lock
	return lock
}

func (s *Service) validOrigin(r *http.Request) bool {
	got, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || got.Path != "" || got.RawQuery != "" || got.Fragment != "" || got.User != nil {
		return false
	}
	normalized, err := canonicalOrigin(got)
	return err == nil && normalized == s.origin
}

func canonicalOrigin(u *url.URL) (string, error) {
	if u == nil || strings.ToLower(u.Scheme) != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid origin")
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "443" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return "https://" + host, nil
}

func (s *Service) validCSRF(r *http.Request, sessionHash []byte) bool {
	cookie, err := r.Cookie(CSRFCookie)
	if err != nil || cookie.Value == "" || r.Header.Get("X-CSRF-Token") == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
		return false
	}
	var expected []byte
	err = s.db.Reader().QueryRowContext(r.Context(), `SELECT csrf_hash FROM browser_sessions WHERE token_hash=? AND revoked_at IS NULL AND expires_at>?`, sessionHash, s.now().UTC().UnixMilli()).Scan(&expected)
	return err == nil && subtle.ConstantTimeCompare(expected, digest(cookie.Value)) == 1
}

func (s *Service) cookie(name, value string, ttl time.Duration, httpOnly bool) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: int(ttl.Seconds()), Expires: s.now().UTC().Add(ttl), Secure: s.secure, HttpOnly: httpOnly, SameSite: http.SameSiteLaxMode}
}

func (s *Service) expiredCookie(name string, httpOnly bool) *http.Cookie {
	return &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), Secure: s.secure, HttpOnly: httpOnly, SameSite: http.SameSiteLaxMode}
}

func (s *Service) seal(tokens storedTokens) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(tokens)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, plain, []byte("receipts/twitch-token/v1"))...), nil
}

func (s *Service) open(sealed []byte, dst *storedTokens) error {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(sealed) < aead.NonceSize() {
		return errors.New("auth: invalid ciphertext")
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte("receipts/twitch-token/v1"))
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, dst)
}

func (s *Service) encryptForTest(tokens storedTokens) ([]byte, error) { return s.seal(tokens) }

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("auth: secure random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func digest(value string) []byte { sum := sha256.Sum256([]byte(value)); return sum[:] }
func cookieValue(c *http.Cookie) string {
	if c == nil {
		return ""
	}
	return c.Value
}

func validLogin(login string) bool {
	if login == "" || len(login) > 25 {
		return false
	}
	for _, r := range login {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func validateScopeSet(actual, required []string) error {
	set := make(map[string]bool, len(actual))
	for _, v := range actual {
		if strings.TrimSpace(v) == "" || strings.ContainsAny(v, " \t\r\n") {
			return ErrForbidden
		}
		set[v] = true
	}
	for _, v := range required {
		if !set[v] {
			return ErrForbidden
		}
	}
	return nil
}

func hasScopes(actual, required []string) bool { return validateScopeSet(actual, required) == nil }

func encodeScopes(scopes []string) string {
	copyScopes := append([]string(nil), scopes...)
	sort.Strings(copyScopes)
	b, _ := json.Marshal(copyScopes)
	return string(b)
}

func decodeScopes(raw string) []string {
	var result []string
	_ = json.Unmarshal([]byte(raw), &result)
	return result
}

func authFailure(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.Error(w, "Twitch sign-in could not be completed. Try connecting again.", status)
}

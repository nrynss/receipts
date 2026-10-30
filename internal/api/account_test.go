package api

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

func TestAccountStopIsScopedAndCSRFProtected(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "accounts.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := auth.New(ctx, auth.Config{DB: db, Provider: unusedHostedProvider{}, ClientID: "app", RedirectURL: "https://receipts.example/auth/twitch/callback", PublicOrigin: "https://receipts.example", EncryptionKey: make([]byte, 32), SecureCookie: true})
	if err != nil {
		t.Fatal(err)
	}
	manager := channelruntime.New(channelruntime.Config{MaxChannels: 2})
	type owner struct {
		session, csrf string
		authority     auth.ChannelContext
	}
	create := func(name string) owner {
		c, err := ledger.CreateChannel(ctx, name, name)
		if err != nil {
			t.Fatal(err)
		}
		live, err := ledger.StartSession(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		id := "account-" + name
		_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES(?,'twitch',?,?,?,X'00',?,'[]','connected',?)`, id, name, name, c.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		session, csrf := "session-"+name, "csrf-"+name
		sh, ch := sha256.Sum256([]byte(session)), sha256.Sum256([]byte(csrf))
		_, err = db.Writer().ExecContext(ctx, `INSERT INTO browser_sessions(token_hash,account_id,csrf_hash,created_at,expires_at) VALUES(?,?,?,?,?)`, sh[:], id, ch[:], now.UnixMilli(), now.Add(time.Hour).UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		a := auth.ChannelContext{AccountID: id, ChannelID: c.ID, LiveSessionID: live.ID, Generation: 1}
		if _, err = manager.StartVerified(ctx, a, ledger); err != nil {
			t.Fatal(err)
		}
		return owner{session, csrf, a}
	}
	a, b := create("a"), create("b")
	handler := AccountHandler(sessions, manager, nil, nil)
	request := func(o owner, withCSRF bool) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "https://receipts.example/account-api/stop?channel="+b.authority.ChannelID, strings.NewReader(""))
		r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: o.session})
		if withCSRF {
			r.AddCookie(&http.Cookie{Name: auth.CSRFCookie, Value: o.csrf})
			r.Header.Set("X-CSRF-Token", o.csrf)
			r.Header.Set("Origin", "https://receipts.example")
		}
		return r
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, request(a, false))
	if res.Code != 403 {
		t.Fatalf("csrf %d", res.Code)
	}
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, request(a, true))
	if res.Code != 204 {
		t.Fatalf("stop %d", res.Code)
	}
	if _, err := manager.Resolve(a.authority); err == nil {
		t.Fatal("owner remains active")
	}
	if _, err := manager.Resolve(b.authority); err != nil {
		t.Fatal("other owner stopped")
	}
	status := httptest.NewRequest(http.MethodGet, "https://receipts.example/account-api/status", nil)
	status.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: a.session})
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, status)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "unavailable") {
		t.Fatalf("offline status %d %s", res.Code, res.Body.String())
	}
	for _, state := range []ListeningState{StateOffline, StateUnknown, StateMissingGrant, StateCapacity, StateNotListening, StateUnavailable, StateListening} {
		scoped := AccountHandlerWithBackend(sessions, manager, nil, nil, fixtureListeningBackend{state: state})
		request := httptest.NewRequest(http.MethodGet, "https://receipts.example/account-api/status", nil)
		request.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: a.session})
		result := httptest.NewRecorder()
		scoped.ServeHTTP(result, request)
		expected := state
		if state == StateListening {
			expected = StateUnknown
		}
		if !strings.Contains(result.Body.String(), string(expected)) {
			t.Fatalf("state %s: %s", state, result.Body.String())
		}
	}
	fakeStart := AccountHandlerWithBackend(sessions, manager, nil, nil, fixtureListeningBackend{state: StateNotListening})
	start := httptest.NewRequest(http.MethodPost, "https://receipts.example/account-api/start", nil)
	start.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: a.session})
	start.AddCookie(&http.Cookie{Name: auth.CSRFCookie, Value: a.csrf})
	start.Header.Set("Origin", "https://receipts.example")
	start.Header.Set("X-CSRF-Token", a.csrf)
	startResult := httptest.NewRecorder()
	fakeStart.ServeHTTP(startResult, start)
	if startResult.Code != http.StatusServiceUnavailable {
		t.Fatalf("false start %d", startResult.Code)
	}
	realStart := AccountHandlerWithBackend(sessions, manager, nil, nil, fixtureListeningBackend{state: StateSessionActive, start: func(_ context.Context, identity auth.ChannelContext) error {
		if identity.AccountID != a.authority.AccountID {
			t.Error("wrong owner")
		}
		_, err := manager.StartVerified(ctx, a.authority, ledger)
		return err
	}})
	resultStart := httptest.NewRecorder()
	realStart.ServeHTTP(resultStart, start)
	if resultStart.Code != http.StatusNoContent {
		t.Fatalf("start %d", resultStart.Code)
	}
	if err := manager.SetPaused(a.authority, true); err != nil {
		t.Fatal(err)
	}
	statusPaused := httptest.NewRequest(http.MethodGet, "https://receipts.example/account-api/status", nil)
	statusPaused.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: a.session})
	pausedResult := httptest.NewRecorder()
	realStart.ServeHTTP(pausedResult, statusPaused)
	if !strings.Contains(pausedResult.Body.String(), "session_active") {
		t.Fatalf("paused: %s", pausedResult.Body.String())
	}
	if err := manager.Stop(a.authority); err != nil {
		t.Fatal(err)
	}
	budgets, err := channelruntime.OpenBudgets(ctx, db, channelruntime.BudgetConfig{GlobalLimit: 100, ChannelLimit: 50})
	if err != nil {
		t.Fatal(err)
	}
	deletion := AccountHandler(sessions, manager, sessions.RevokeProviderAuthorization, budgets)
	del := httptest.NewRequest(http.MethodPost, "https://receipts.example/account-api/delete", nil)
	del.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: a.session})
	del.AddCookie(&http.Cookie{Name: auth.CSRFCookie, Value: a.csrf})
	del.Header.Set("Origin", "https://receipts.example")
	del.Header.Set("X-CSRF-Token", a.csrf)
	result := httptest.NewRecorder()
	deletion.ServeHTTP(result, del)
	if result.Code != http.StatusNoContent {
		t.Fatalf("delete %d %s", result.Code, result.Body.String())
	}
	if _, err := manager.Resolve(b.authority); err != nil {
		t.Fatal("deletion stopped other owner")
	}
	var remaining int
	if err := db.Reader().QueryRowContext(ctx, "SELECT count(*) FROM accounts WHERE id=?", b.authority.AccountID).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatal("deletion removed other owner")
	}

}

type fixtureListeningBackend struct {
	state ListeningState
	start func(context.Context, auth.ChannelContext) error
}

func (f fixtureListeningBackend) Status(context.Context, auth.ChannelContext) (ListeningState, error) {
	return f.state, nil
}
func (f fixtureListeningBackend) Start(ctx context.Context, a auth.ChannelContext) error {
	if f.start != nil {
		return f.start(ctx, a)
	}
	return nil
}

func (f fixtureListeningBackend) CanStart() bool { return f.start != nil }

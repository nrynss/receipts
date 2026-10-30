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
	if res.Code != 200 || !strings.Contains(res.Body.String(), "not_listening") {
		t.Fatalf("offline status %d %s", res.Code, res.Body.String())
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

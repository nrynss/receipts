package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	"receipts/internal/events"
	"receipts/internal/receipt"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

type unusedHostedProvider struct{}

func (unusedHostedProvider) AuthorizeURL(string, string, []string) string {
	panic("no provider calls in composition test")
}
func (unusedHostedProvider) ExchangeCode(context.Context, string, string) (auth.Tokens, error) {
	panic("no provider calls in composition test")
}
func (unusedHostedProvider) Validate(context.Context, string) (auth.Grant, error) {
	panic("no provider calls in composition test")
}
func (unusedHostedProvider) Refresh(context.Context, string) (auth.Tokens, error) {
	panic("no provider calls in composition test")
}

func TestProductionHostedCompositionScopesSessionsDevicesAndRevocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Now().UTC()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "hosted.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := auth.New(ctx, auth.Config{DB: db, Provider: unusedHostedProvider{}, ClientID: "test-app", RedirectURL: "https://receipts.example/auth/twitch/callback", PublicOrigin: "https://receipts.example", EncryptionKey: make([]byte, 32), SecureCookie: true, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	manager := channelruntime.New(channelruntime.Config{MaxChannels: 2})
	sessions.OnDisconnect(manager.RevokeAccount)
	type owner struct {
		authority     auth.ChannelContext
		session, csrf string
		channel       *channelruntime.Channel
	}
	create := func(name string) owner {
		t.Helper()
		c, err := ledger.CreateChannel(ctx, "platform-"+name, name)
		if err != nil {
			t.Fatal(err)
		}
		live, err := ledger.StartSession(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		accountID := "account-" + name
		_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES(?,'twitch',?,?,?,X'00',?,'[]','connected',?)`, accountID, "platform-"+name, name, c.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		raw, csrf := "test-session-"+name, "test-csrf-"+name
		sh, ch := sha256.Sum256([]byte(raw)), sha256.Sum256([]byte(csrf))
		_, err = db.Writer().ExecContext(ctx, `INSERT INTO browser_sessions(token_hash,account_id,csrf_hash,created_at,expires_at) VALUES(?,?,?,?,?)`, sh[:], accountID, ch[:], now.UnixMilli(), now.Add(time.Hour).UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		a := auth.ChannelContext{AccountID: accountID, ChannelID: c.ID, BrowserSessionID: hex.EncodeToString(sh[:]), LiveSessionID: live.ID, Generation: 1}
		running, err := manager.StartVerified(ctx, a, ledger)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = manager.Stop(a) })
		seg := receipt.Segment{ID: "source-" + name, SessionID: live.ID, Text: "I will never take that shortcut again.", Start: 0, End: time.Second, Attribution: "broadcaster"}
		if err := ledger.AddSegment(ctx, seg); err != nil {
			t.Fatal(err)
		}
		if _, err := ledger.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: live.ID, Claim: name + "-promise", ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{{SourceID: seg.ID, Text: seg.Text, Start: seg.Start, End: seg.End, Attribution: seg.Attribution}}}); err != nil {
			t.Fatal(err)
		}
		return owner{a, raw, csrf, running}
	}
	a, b := create("a"), create("b")
	handler := NewForOwner(Config{OwnerReader: LedgerReader{Store: ledger, Mode: "live"}, OwnerControls: LedgerControls{Store: ledger, Runtime: manager}}, sessions, manager)
	req := func(o owner, method, path, body string, csrf bool) *http.Request {
		r := httptest.NewRequest(method, "https://receipts.example"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: o.session})
		if csrf {
			r.AddCookie(&http.Cookie{Name: auth.CSRFCookie, Value: o.csrf})
			r.Header.Set("X-CSRF-Token", o.csrf)
			r.Header.Set("Origin", "https://receipts.example")
		}
		return r
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req(a, http.MethodGet, "/api/overlay?channel_id="+b.authority.ChannelID, "", false))
	var snapshot events.OverlaySnapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || snapshot.Channel != a.authority.ChannelID || len(snapshot.Receipts) != 1 || snapshot.Receipts[0].Claim != "a-promise" {
		t.Fatalf("snapshot status=%d value=%+v", response.Code, snapshot)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req(a, http.MethodPost, "/api/control/pause", `{"paused":true}`, false))
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req(a, http.MethodPost, "/api/control/pause?channel_id="+b.authority.ChannelID, `{"paused":true}`, true))
	if response.Code != http.StatusNoContent || !a.channel.Paused() || b.channel.Paused() {
		t.Fatalf("pause status=%d A=%v B=%v", response.Code, a.channel.Paused(), b.channel.Paused())
	}
	_, device, err := sessions.IssueDeviceCredential(ctx, a.authority)
	if err != nil {
		t.Fatal(err)
	}
	deviceReq := httptest.NewRequest(http.MethodPost, "https://receipts.example/api/control/pause", strings.NewReader(`{"paused":true}`))
	deviceReq.Header.Set("Authorization", "Bearer "+device)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, deviceReq)
	if response.Code != http.StatusForbidden {
		t.Fatalf("device control status=%d", response.Code)
	}
	streamCtx, stopStream := context.WithCancel(ctx)
	defer stopStream()
	stream := httptest.NewRequest(http.MethodGet, "https://receipts.example/api/events", nil).WithContext(streamCtx)
	stream.Header.Set("Authorization", "Bearer "+device)
	writer := newSyncedWriter()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(writer, stream); close(done) }()
	deadline := time.After(time.Second)
	for !strings.Contains(writer.Text(), "reset") {
		select {
		case <-writer.changed:
		case <-deadline:
			t.Fatal("device SSE never subscribed")
		}
	}
	if err := sessions.RevokeProviderAuthorization(ctx, a.authority.AccountID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("provider revocation failed to cancel device SSE")
	}
	if _, err := manager.Resolve(a.authority); err == nil {
		t.Fatal("revoked runtime still resolved")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req(b, http.MethodGet, "/api/overlay", "", false))
	if response.Code != 200 {
		t.Fatalf("revoking A affected B: status=%d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req(a, http.MethodGet, "/api/overlay", "", false))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked owner status=%d", response.Code)
	}
}

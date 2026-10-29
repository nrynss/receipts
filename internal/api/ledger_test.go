package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	"receipts/internal/receipt"
	"receipts/internal/store"
)

func TestLedgerReaderScopesStoredDataAndFiltersPrivateClaims(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	create := func(login string) (auth.ChannelContext, receipt.Receipt, string) {
		t.Helper()
		channel, err := ledger.CreateChannel(ctx, "platform-"+login, login)
		if err != nil {
			t.Fatal(err)
		}
		session, err := ledger.StartSession(ctx, channel.ID)
		if err != nil {
			t.Fatal(err)
		}
		accountID := "owner-" + login
		_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES(?,'twitch',?,?,?,X'00',?,'[]','connected',?)`, accountID, "platform-"+login, login, channel.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		seg := receipt.Segment{ID: "source-" + login, SessionID: session.ID, Start: 0, End: time.Second, Text: "I will never take that shortcut again.", Attribution: "broadcaster"}
		if err := ledger.AddSegment(ctx, seg); err != nil {
			t.Fatal(err)
		}
		ev := receipt.EvidenceInput{SourceID: seg.ID, Text: seg.Text, Start: seg.Start, End: seg.End, Attribution: seg.Attribution}
		r, err := ledger.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: channel.ID, SessionID: session.ID, Claim: login + "-claim", ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{ev}})
		if err != nil {
			t.Fatal(err)
		}
		detection, err := ledger.RecordDetection(ctx, receipt.DetectionInput{ChannelID: channel.ID, ReceiptID: r.ID, Evidence: ev, Outcome: receipt.NeedsReview})
		if err != nil {
			t.Fatal(err)
		}
		_ = detection
		return auth.ChannelContext{AccountID: accountID, ChannelID: channel.ID, LiveSessionID: session.ID, Generation: 1}, r, seg.ID
	}
	a, ra, source := create("a")
	b, rb, _ := create("b")
	reader := LedgerReader{Store: ledger, Mode: "live"}
	request := func(principal auth.ChannelContext) *http.Request {
		return httptest.NewRequest(http.MethodGet, "/api/overlay?channel_id="+b.ChannelID, nil).WithContext(auth.WithChannelContext(ctx, principal))
	}
	snapshot, err := reader.OverlaySnapshotFor(request(a), a)
	if err != nil || snapshot.Channel != a.ChannelID || len(snapshot.Receipts) != 1 || snapshot.Receipts[0].ID != ra.ID {
		t.Fatalf("own snapshot=%+v err=%v", snapshot, err)
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), rb.Claim) || strings.Contains(string(encoded), source) || strings.Contains(string(encoded), "transcript") {
		t.Fatal("overlay leaked foreign or internal evidence")
	}
	reviews, err := reader.ReviewQueueFor(request(a), a)
	if err != nil || len(reviews) != 1 || reviews[0].Claim != ra.Claim {
		t.Fatalf("reviews=%+v err=%v", reviews, err)
	}
	if _, err := reader.OverlaySnapshotFor(request(a), b); err == nil {
		t.Fatal("accepted mismatched request authority")
	}
	forged := a
	forged.AccountID = b.AccountID
	if _, err := reader.OverlaySnapshotFor(request(forged), forged); err == nil {
		t.Fatal("accepted guessed durable ownership")
	}
	device := a
	device.Device = true
	if _, err := reader.ReviewQueueFor(request(device), device); err == nil {
		t.Fatal("device accessed review")
	}
	if _, err := reader.HealthFor(request(device), device); err == nil {
		t.Fatal("device accessed private health")
	}
	if err := ledger.Void(ctx, a.ChannelID, ra.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = reader.OverlaySnapshotFor(request(a), a)
	if err != nil || len(snapshot.Receipts) != 0 {
		t.Fatalf("void snapshot=%+v err=%v", snapshot, err)
	}
	reviews, err = reader.ReviewQueueFor(request(a), a)
	if err != nil || len(reviews) != 0 {
		t.Fatalf("void reviews=%+v err=%v", reviews, err)
	}
	if _, err := db.Writer().ExecContext(ctx, `UPDATE receipts SET sensitive=1 WHERE id=?`, rb.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = reader.OverlaySnapshotFor(request(b), b)
	if err != nil || len(snapshot.Receipts) != 0 {
		t.Fatalf("sensitive snapshot=%+v err=%v", snapshot, err)
	}
	if _, err := db.Writer().ExecContext(ctx, `UPDATE accounts SET status='reconnect_required' WHERE id=?`, b.AccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.OverlaySnapshotFor(request(b), b); err == nil {
		t.Fatal("disconnected account read data")
	}
}

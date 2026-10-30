package lore_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/appeal"
	"receipts/internal/auth"
	"receipts/internal/commands"
	"receipts/internal/detect"
	"receipts/internal/lore"
	"receipts/internal/ports"
	"receipts/internal/receipt"
	"receipts/internal/replay"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

type extraction struct{}

func (extraction) Extract(context.Context, []receipt.Segment) (commands.Extraction, error) {
	return commands.Extraction{Claim: "Never take the shortcut again.", SourceIDs: []string{"original"}}, nil
}

type safe struct{}

func (safe) Sensitive(context.Context, string) (bool, error) { return false, nil }

type judge struct{}

func (judge) Judge(context.Context, receipt.Receipt, receipt.Segment) (detect.Verdict, error) {
	return detect.Contradicts, nil
}

type failedClip struct{}

func (failedClip) Enqueue(context.Context, string, string, string, time.Time) error {
	return errors.New("offline clip failure")
}
func run(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()
	clock := replay.NewClock(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	ids := replay.IDs()
	path := filepath.Join(t.TempDir(), "ledger.db")
	open := func() (*sqlite.DB, *store.Store) {
		db, e := sqlite.Open(ctx, sqlite.Config{Path: path, Now: clock.Now})
		if e != nil {
			t.Fatal(e)
		}
		s, e := store.NewWithIDSource(ctx, db, store.Retention{}, clock.Now, ids)
		if e != nil {
			t.Fatal(e)
		}
		return db, s
	}
	db, s := open()
	c, e := s.CreateChannel(ctx, "fixture", "fixture")
	if e != nil {
		t.Fatal(e)
	}
	live, e := s.StartSession(ctx, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','fixture',?,X'01',0,'','connected',0)`, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	original := receipt.Segment{ID: "original", SessionID: live.ID, Text: "I will never take the shortcut again.", Attribution: "broadcaster", End: time.Second}
	if e = s.AddSegment(ctx, original); e != nil {
		t.Fatal(e)
	}
	_ = clock.Advance(clock.Now().Add(2 * time.Second))
	capture := commands.Service{Ledger: s, Extractor: extraction{}, Sensitive: safe{}, Clips: failedClip{}, Now: clock.Now}
	got, e := capture.Handle(ctx, commands.Authority{ChannelID: c.ID, SessionID: live.ID}, ports.ChatCommand{ID: "capture", UserID: "viewer", Login: "viewer", Command: "!receipt", ReceivedAt: clock.Now()})
	if e != nil || got.Receipt == nil {
		t.Fatalf("capture: %+v %v", got, e)
	}
	rid := got.Receipt.ID
	if e = s.CompleteClipJob(ctx, c.ID, "capture", "failed", "", "", "offline"); e != nil {
		t.Fatal(e)
	}
	a := auth.ChannelContext{AccountID: "owner", ChannelID: c.ID, LiveSessionID: live.ID, Generation: 1}
	manager := channelruntime.New(channelruntime.Config{})
	ch, e := manager.StartVerified(ctx, a, s)
	if e != nil {
		t.Fatal(e)
	}
	later := receipt.Segment{ID: "later", SessionID: live.ID, Text: "Fine, I'll take the shortcut.", Attribution: "broadcaster", Start: 2 * time.Second, End: 3 * time.Second}
	if e = s.AddSegment(ctx, later); e != nil {
		t.Fatal(e)
	}
	_ = clock.Advance(clock.Now().Add(time.Second))
	d := detect.Service{Store: s, Runtime: manager, Judge: judge{}, Sensitive: safe{}, Now: clock.Now}
	if e = d.Check(ctx, a, rid, later); e != nil {
		t.Fatal(e)
	}
	ap := appeal.Service{Store: s, Runtime: manager, Duration: time.Minute, Now: clock.Now}
	if e = ap.Answer(ctx, a, rid, "answer", "appeal", ""); e != nil {
		t.Fatal(e)
	}
	if e = ap.Vote(ctx, a, "voter", receipt.VoteUphold); e != nil {
		t.Fatal(e)
	}
	before := ch.Events.Sequence()
	if e = manager.Stop(a); e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	_ = clock.Advance(clock.Now().Add(time.Minute))
	db, s = open()
	defer db.Close()
	manager = channelruntime.New(channelruntime.Config{})
	ch, e = manager.StartVerified(ctx, a, s)
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Stop(a)
	ap = appeal.Service{Store: s, Runtime: manager, Duration: time.Minute, Now: clock.Now}
	if e = ap.Recover(ctx, a); e != nil {
		t.Fatal(e)
	}
	text, e := (lore.Service{Ledger: s}).Render(ctx, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(text, "1 total | 0 open, 1 broken") || !strings.Contains(text, "Never take the shortcut") {
		t.Fatal(text)
	}
	var outcome string
	if e = db.Reader().QueryRowContext(ctx, `SELECT outcome FROM verdicts WHERE receipt_id=?`, rid).Scan(&outcome); e != nil || outcome != "upheld" {
		t.Fatalf("verdict %s %v", outcome, e)
	}
	job, e := s.ClipJobForReceipt(ctx, c.ID, rid)
	if e != nil || job.Status != "failed" {
		t.Fatalf("clip %+v %v", job, e)
	}
	result := []string{rid, outcome, text, job.Status, strings.Repeat("e", int(before)), strings.Repeat("e", int(ch.Events.Sequence()))}
	for _, table := range []string{"receipts", "receipt_commands", "receipt_captures", "clip_jobs", "detections", "appeals", "votes", "verdicts", "receipt_actions"} {
		rows, err := db.Reader().QueryContext(ctx, "SELECT * FROM "+table+" ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptr := make([]any, len(cols))
			for i := range vals {
				ptr[i] = &vals[i]
			}
			if err = rows.Scan(ptr...); err != nil {
				t.Fatal(err)
			}
			result = append(result, fmt.Sprint(table, vals))
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	if _, e = db.Writer().ExecContext(ctx, `UPDATE receipts SET sensitive=1 WHERE id=?`, rid); e != nil {
		t.Fatal(e)
	}
	hidden, e := (lore.Service{Ledger: s}).Render(ctx, c.ID)
	if e != nil || strings.Contains(hidden, "shortcut") || !strings.Contains(hidden, "0 total") {
		t.Fatalf("sensitive leak: %s %v", hidden, e)
	}
	if _, e = db.Writer().ExecContext(ctx, `UPDATE receipts SET sensitive=0,validity='void' WHERE id=?`, rid); e != nil {
		t.Fatal(e)
	}
	hidden, e = (lore.Service{Ledger: s}).Render(ctx, c.ID)
	if e != nil || strings.Contains(hidden, "shortcut") || !strings.Contains(hidden, "0 total") {
		t.Fatalf("void leak: %s %v", hidden, e)
	}
	return result
}
func TestOfflineScenario(t *testing.T) {
	a, b := run(t), run(t)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("runs differ: %v %v", a, b)
	}
}

type rowsLedger []receipt.Receipt

func (r rowsLedger) LoreReceipts(context.Context, string) ([]receipt.Receipt, error) { return r, nil }
func TestScoreAndBound(t *testing.T) {
	rows := rowsLedger{}
	for _, v := range []receipt.Verdict{receipt.Undecided, receipt.Admitted, receipt.Upheld, receipt.Overturned, receipt.ContextChanged, receipt.Inconclusive} {
		rows = append(rows, receipt.Receipt{Verdict: v, Claim: strings.Repeat("界", 300)})
	}
	text, err := (lore.Service{Ledger: rows}).Render(context.Background(), "channel")
	if err != nil {
		t.Fatal(err)
	}
	if len(text) > 450 || !utf8.ValidString(text) || !strings.Contains(text, "6 total | 1 open, 2 broken, 1 overturned, 1 context changed, 1 inconclusive") {
		t.Fatal(text)
	}
}

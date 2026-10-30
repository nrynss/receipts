package lore_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nrynss/keel/sqlite"
	"github.com/nrynss/keel/stream"
	"receipts/internal/appeal"
	"receipts/internal/auth"
	"receipts/internal/commands"
	"receipts/internal/detect"
	"receipts/internal/events"
	"receipts/internal/lore"
	"receipts/internal/ports"
	"receipts/internal/receipt"
	"receipts/internal/replay"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

type extraction struct{}

func (extraction) Extract(context.Context, []receipt.Segment) (commands.Extraction, error) {
	return commands.Extraction{Claim: "Never take the shortcut again.", SourceIDs: []string{"segment-receipt-1"}}, nil
}

type safe struct{}

func (safe) Sensitive(context.Context, string) (bool, error) { return false, nil }

type transcriptSink func(context.Context, ports.TranscriptSegment) error

func (f transcriptSink) AcceptTranscript(ctx context.Context, v ports.TranscriptSegment) error {
	return f(ctx, v)
}

type chatSink func(context.Context, ports.ChatCommand) error

func (f chatSink) AcceptChatCommand(ctx context.Context, v ports.ChatCommand) error { return f(ctx, v) }

type clipSink func(context.Context, ports.ClipResult) error

func (f clipSink) AcceptClipResult(ctx context.Context, v ports.ClipResult) error { return f(ctx, v) }
func fixture(t *testing.T, name string) replay.Fixture {
	t.Helper()
	f, e := os.Open(name)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	v, e := replay.Decode(f)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func one(t *testing.T, f replay.Fixture, i int, sinks replay.Sinks, clock *replay.Clock) {
	t.Helper()
	f.Inputs = f.Inputs[i : i+1]
	if err := f.Run(context.Background(), sinks, clock); err != nil {
		t.Fatal(err)
	}
}

type failedClip struct{}

func (failedClip) Enqueue(context.Context, string, string, string, time.Time) error {
	return errors.New("offline clip failure")
}
func run(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()
	clock := replay.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
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
	captureFixture := fixture(t, "../../testdata/replay/t21-capture.json")
	capture := commands.Service{Ledger: s, Extractor: extraction{}, Sensitive: safe{}, Clips: failedClip{}, Now: clock.Now}
	var got commands.Result
	transcript := transcriptSink(func(ctx context.Context, v ports.TranscriptSegment) error {
		return s.AddSegment(ctx, receipt.Segment{ID: v.ID, SessionID: v.SessionID, Start: v.Start, End: v.End, Text: v.Text, SpeakerID: v.SpeakerID, Attribution: string(v.Attribution)})
	})
	captureSink := commands.ChatSink{Service: &capture, Authority: commands.Authority{ChannelID: c.ID, SessionID: live.ID}, OnResult: func(r commands.Result) { got = r }}
	if e = captureFixture.Run(ctx, replay.Sinks{Transcript: transcript, Chat: captureSink}, clock); e != nil {
		t.Fatal(e)
	}
	if got.Receipt == nil || got.Code != commands.ReplyCaptured {
		t.Fatalf("capture: %+v", got)
	}
	rid := got.Receipt.ID
	loop := fixture(t, "../../testdata/replay/t24-loop.json")
	one(t, loop, 0, replay.Sinks{Clip: clipSink(func(ctx context.Context, v ports.ClipResult) error {
		return s.CompleteClipJob(ctx, c.ID, v.RequestID, string(v.Status), "", "", "offline")
	})}, clock)
	a := auth.ChannelContext{AccountID: "owner", ChannelID: c.ID, LiveSessionID: live.ID, Generation: 1}
	manager := channelruntime.New(channelruntime.Config{})
	ch, e := manager.StartVerified(ctx, a, s)
	if e != nil {
		t.Fatal(e)
	}
	ch.Events = events.NewPublisher("before-restart")
	sub := ch.Events.Broker.Subscribe(ctx, events.TopicOverlay)
	judgeFile, e := os.Open("../../testdata/models/t22-judge.json")
	if e != nil {
		t.Fatal(e)
	}
	recorded, e := detect.LoadRecorded(judgeFile)
	judgeFile.Close()
	if e != nil {
		t.Fatal(e)
	}
	d := detect.Service{Store: s, Runtime: manager, Judge: recorded, Sensitive: safe{}, Now: clock.Now}
	one(t, loop, 1, replay.Sinks{Transcript: transcriptSink(func(ctx context.Context, v ports.TranscriptSegment) error {
		seg := receipt.Segment{ID: v.ID, SessionID: v.SessionID, Text: v.Text, Attribution: string(v.Attribution), SpeakerID: v.SpeakerID, Start: v.Start, End: v.End}
		if err := s.AddSegment(ctx, seg); err != nil {
			return err
		}
		return d.Check(ctx, a, rid, seg)
	})}, clock)
	ap := appeal.Service{Store: s, Runtime: manager, Duration: time.Minute, Now: clock.Now}
	if e = ap.Answer(ctx, a, rid, "answer", "appeal", ""); e != nil {
		t.Fatal(e)
	}
	one(t, loop, 2, replay.Sinks{Chat: appeal.ChatSink{Service: ap, Authority: a}}, clock)
	var emitted []string
	for _, want := range []string{events.Stamp, events.Status, events.Status} {
		var ev stream.Event
		select {
		case ev = <-sub.Events:
		default:
			t.Fatal("missing event")
		}
		if ev.Name != want {
			t.Fatalf("event name %s want %s", ev.Name, want)
		}
		b, err := json.Marshal(ev.Data)
		if err != nil {
			t.Fatal(err)
		}
		emitted = append(emitted, ev.Name+":"+string(b))
	}
	sub.Cancel()
	before := ch.Events.Sequence()
	if e = manager.Stop(a); e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	_ = clock.Advance(loop.Inputs[3].At)
	db, s = open()
	defer db.Close()
	manager = channelruntime.New(channelruntime.Config{})
	ch, e = manager.StartVerified(ctx, a, s)
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Stop(a)
	ch.Events = events.NewPublisher("after-restart")
	sub = ch.Events.Broker.Subscribe(ctx, events.TopicOverlay)
	defer sub.Cancel()
	ap = appeal.Service{Store: s, Runtime: manager, Duration: time.Minute, Now: clock.Now}
	if e = ap.Recover(ctx, a); e != nil {
		t.Fatal(e)
	}
	var ev stream.Event
	select {
	case ev = <-sub.Events:
	default:
		t.Fatal("missing event")
	}
	if ev.Name != events.Status {
		t.Fatalf("recovery event %s", ev.Name)
	}
	b, err := json.Marshal(ev.Data)
	if err != nil {
		t.Fatal(err)
	}
	emitted = append(emitted, ev.Name+":"+string(b))
	var text string
	one(t, loop, 3, replay.Sinks{Chat: chatSink(func(ctx context.Context, cmd ports.ChatCommand) error {
		var err error
		text, err = (lore.Service{Ledger: s}).Handle(ctx, c.ID, cmd)
		return err
	})}, clock)
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
	t.Logf("events: %v", emitted)
	result = append(result, emitted...)
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
	if _, e = db.Writer().ExecContext(ctx, `UPDATE receipts SET validity='valid',expires_at=? WHERE id=?`, clock.Now().UnixMilli(), rid); e != nil {
		t.Fatal(e)
	}
	hidden, e = (lore.Service{Ledger: s}).Render(ctx, c.ID)
	if e != nil || strings.Contains(hidden, "shortcut") || !strings.Contains(hidden, "0 total") {
		t.Fatalf("expired leak: %s %v", hidden, e)
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
		rows = append(rows, receipt.Receipt{Verdict: v, Claim: "a promise"})
	}
	text, err := (lore.Service{Ledger: rows}).Render(context.Background(), "channel")
	if err != nil {
		t.Fatal(err)
	}
	if len(text) > 450 || !utf8.ValidString(text) || !strings.Contains(text, "6 total | 1 open, 2 broken, 1 overturned, 1 context changed, 1 inconclusive") {
		t.Fatal(text)
	}
	for _, tag := range []string{"(open)", "(broken)", "(overturned)", "(context changed)", "(inconclusive)"} {
		if !strings.Contains(text, tag) {
			t.Fatalf("missing %s in %q", tag, text)
		}
	}
	long, err := (lore.Service{Ledger: rowsLedger{{Verdict: receipt.Undecided, Claim: strings.Repeat("界", 300)}}}).Render(context.Background(), "channel")
	if err != nil || len(long) > 450 || !utf8.ValidString(long) || !strings.Contains(long, "(open)") {
		t.Fatalf("bounded output %q, err=%v", long, err)
	}
}

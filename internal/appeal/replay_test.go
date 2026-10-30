package appeal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	"receipts/internal/receipt"
	"receipts/internal/replay"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

func runRecordedVotes(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := replay.NewClock(start)
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "replay.db"), Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ledger, err := store.NewWithIDSource(ctx, db, store.Retention{}, clock.Now, replay.IDs())
	if err != nil {
		t.Fatal(err)
	}
	channel, err := ledger.CreateChannel(ctx, "fixture-channel", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	live, err := ledger.StartSession(ctx, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','fixture',?,X'01',0,'','connected',0)`, channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	segment := receipt.Segment{ID: "source", SessionID: live.ID, Text: "I will return", Attribution: "broadcaster", Start: 0, End: time.Second}
	if err = ledger.AddSegment(ctx, segment); err != nil {
		t.Fatal(err)
	}
	evidence := receipt.EvidenceInput{SourceID: segment.ID, Text: segment.Text, Start: segment.Start, End: segment.End, Attribution: segment.Attribution}
	r, err := ledger.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: channel.ID, SessionID: live.ID, Claim: "Will return", ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{evidence}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ledger.RecordDetection(ctx, receipt.DetectionInput{ChannelID: channel.ID, ReceiptID: r.ID, Evidence: evidence, Outcome: receipt.Stamp}); err != nil {
		t.Fatal(err)
	}
	a := auth.ChannelContext{AccountID: "owner", ChannelID: channel.ID, LiveSessionID: live.ID, Generation: 1}
	manager := channelruntime.New(channelruntime.Config{})
	ch, err := manager.StartVerified(ctx, a, ledger)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(a)
	svc := Service{Store: ledger, Runtime: manager, Duration: time.Minute, Now: clock.Now}
	if err = svc.Answer(ctx, a, r.ID, "action", "appeal", ""); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open("../../testdata/replay/t23-votes.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	fixture, err := replay.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = fixture.Run(ctx, replay.Sinks{Chat: ChatSink{Service: svc, Authority: a}}, clock); err != nil {
		t.Fatal(err)
	}
	public, err := ledger.ActiveAppeal(ctx, channel.ID)
	if err != nil || public == nil || public.Public.UpholdVotes != 1 || public.Public.OverturnVotes != 1 {
		t.Fatalf("counts %#v %v", public, err)
	}
	if err = clock.Advance(start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = svc.Recover(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := ledger.GetReceipt(ctx, r.ID)
	if err != nil || got.Verdict != receipt.Inconclusive {
		t.Fatalf("verdict %v %v", got.Verdict, err)
	}
	if ch.Events.Sequence() != 4 {
		t.Fatalf("expected appeal, votes and verdict events; got %d", ch.Events.Sequence())
	}
	return string(got.Verdict) + ":" + public.ID + ":" + r.ID
}

func TestRecordedVotesUseApplicationAndLedger(t *testing.T) {
	first := runRecordedVotes(t)
	second := runRecordedVotes(t)
	if first != second {
		t.Fatalf("replay differs: %s != %s", first, second)
	}
}

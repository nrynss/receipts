package intelligence

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	"receipts/internal/commands"
	"receipts/internal/detect"
	"receipts/internal/judge"
	"receipts/internal/ports"
	"receipts/internal/receipt"
	"receipts/internal/retrieval"
	runtime "receipts/internal/runtime"
	"receipts/internal/store"
)

type judgeFunc func(context.Context, receipt.Receipt, receipt.Evidence, receipt.Segment) (judge.Result, error)

func (f judgeFunc) Judge(ctx context.Context, r receipt.Receipt, e receipt.Evidence, s receipt.Segment) (judge.Result, error) {
	return f(ctx, r, e, s)
}

type extractFunc func(context.Context, []receipt.Segment) (commands.Extraction, error)

func (f extractFunc) Extract(c context.Context, s []receipt.Segment) (commands.Extraction, error) {
	return f(c, s)
}

type sensitiveFunc func(context.Context, string) (bool, error)

func (f sensitiveFunc) Sensitive(c context.Context, s string) (bool, error) { return f(c, s) }

type embedFunc func(context.Context, string) ([]float64, error)

func (f embedFunc) Embed(c context.Context, s string) ([]float64, error) { return f(c, s) }

func TestCaptureReindexAndRetrieveWithDurableBudget(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC().Add(-10 * time.Second).Truncate(time.Millisecond)
	ledger, err := store.New(ctx, db, store.Retention{}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	c, err := ledger.CreateChannel(ctx, "streamer", "streamer")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','streamer','streamer',?,X'00',?,'[]','connected',?)`, c.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	activation, err := ledger.StartOwnedLiveSession(ctx, "owner", c.ID, "broadcast", now, false)
	if err != nil {
		t.Fatal(err)
	}
	a := auth.ChannelContext{AccountID: "owner", ChannelID: c.ID, LiveSessionID: activation.Session.ID, Generation: activation.Generation}
	manager := runtime.New(runtime.Config{})
	budgets, err := runtime.OpenBudgets(ctx, db, runtime.BudgetConfig{GlobalLimit: cost.Price(2_000_000_000), ChannelLimit: cost.Price(2_000_000_000)})
	if err != nil {
		t.Fatal(err)
	}
	manager.SetBudgets(budgets)
	if _, err = manager.StartVerified(ctx, a, ledger); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(a)
	original := receipt.Segment{ID: "original", SessionID: a.LiveSessionID, Start: time.Second, End: 2 * time.Second, Text: "I will never play this map.", SpeakerID: c.ID, Attribution: "broadcaster", TimingBasis: "coarse_pcm_ingest_hls_delay_unknown"}
	if err = ledger.AddLiveSegment(ctx, a.AccountID, a.ChannelID, a.LiveSessionID, a.Generation, original); err != nil {
		t.Fatal(err)
	}
	now = time.Now().UTC().Truncate(time.Millisecond)
	calls := 0
	sourceChecks := 0
	svc := &Service{Authority: a, Meter: manager, Ledger: ledger, Index: ledger, Space: retrieval.Space{Model: retrieval.DefaultModel, Version: "v1", Dimensions: 2}, ExtractionModel: "gpt-4o-mini-2024-07-18", Extractor: extractFunc(func(_ context.Context, segs []receipt.Segment) (commands.Extraction, error) {
		calls++
		return commands.Extraction{Claim: segs[0].Text, SourceIDs: []string{segs[0].ID}}, nil
	}), Sensitive: sensitiveFunc(func(_ context.Context, text string) (bool, error) { sourceChecks++; return false, nil }), Embedder: embedFunc(func(context.Context, string) ([]float64, error) { return []float64{1, 0}, nil })}
	cmd := ports.ChatCommand{ID: "command", UserID: "viewer", Login: "viewer", Command: "!receipt", ReceivedAt: now}
	result, err := svc.Handle(ctx, commands.Authority{ChannelID: c.ID, SessionID: a.LiveSessionID}, cmd)
	if err != nil || result.Code != commands.ReplyCaptured || sourceChecks != 2 {
		t.Fatalf("capture=%#v checks=%d err=%v", result, sourceChecks, err)
	}
	evidence, err := ledger.Evidence(ctx, result.Receipt.ID)
	if err != nil || len(evidence) != 1 || evidence[0].TimingBasis != "coarse_pcm_ingest_hls_delay_unknown" {
		t.Fatalf("provenance=%#v %v", evidence, err)
	}
	if err = svc.Reindex(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	later := receipt.Segment{ID: "later", SessionID: a.LiveSessionID, Start: 11 * time.Second, End: 12 * time.Second, Text: "I will play this map.", SpeakerID: c.ID, Attribution: "broadcaster", TimingBasis: "coarse_pcm_ingest_hls_delay_unknown"}
	if err = ledger.AddLiveSegment(ctx, a.AccountID, a.ChannelID, a.LiveSessionID, a.Generation, later); err != nil {
		t.Fatal(err)
	}
	matches, err := svc.Nearest(ctx, later, 3)
	if err != nil || len(matches) != 1 || matches[0].Receipt.ID != result.Receipt.ID {
		t.Fatalf("matches=%#v err=%v", matches, err)
	}
	svc.DetectionStore = ledger
	svc.Judge = judgeFunc(func(_ context.Context, _ receipt.Receipt, e receipt.Evidence, src receipt.Segment) (judge.Result, error) {
		return judge.Result{Verdict: detect.Contradicts, ClaimSourceID: e.SourceID, LaterSourceID: src.ID}, nil
	})
	if err = svc.JudgeSource(ctx, later); err != nil {
		t.Fatalf("judge source: %v", err)
	}
	var outcome string
	if err = db.Reader().QueryRowContext(ctx, `SELECT outcome FROM detections WHERE receipt_id=? AND source_id=?`, result.Receipt.ID, later.ID).Scan(&outcome); err != nil || outcome != "review" {
		t.Fatalf("uncalibrated outcome=%q err=%v", outcome, err)
	}
	later2 := receipt.Segment{ID: "later2", SessionID: a.LiveSessionID, Start: 13 * time.Second, End: 14 * time.Second, Text: "I will play again.", SpeakerID: c.ID, Attribution: "broadcaster", TimingBasis: "coarse_pcm_ingest_hls_delay_unknown"}
	if err = ledger.AddLiveSegment(ctx, a.AccountID, a.ChannelID, a.LiveSessionID, a.Generation, later2); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	svc.Judge = judgeFunc(func(_ context.Context, _ receipt.Receipt, e receipt.Evidence, src receipt.Segment) (judge.Result, error) {
		cancel()
		return judge.Result{Verdict: detect.Contradicts, ClaimSourceID: e.SourceID, LaterSourceID: src.ID}, nil
	})
	if err = svc.JudgeSource(canceled, later2); err == nil {
		t.Fatal("late judgment succeeded")
	}
	var count int
	if err = db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM detections WHERE source_id=?`, later2.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("late detections=%d err=%v", count, err)
	}
	_, err = ledger.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: c.ID, SessionID: a.LiveSessionID, Claim: "Another promise", ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{{SourceID: original.ID, Text: original.Text, Start: original.Start, End: original.End, Attribution: original.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Nearest(ctx, later, 3); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("incomplete index: %v", err)
	}
	budgetCtx := auth.WithChannelContext(ctx, a)
	channel, global, err := budgets.Remaining(budgetCtx, c.ID)
	if err != nil || channel >= 2_000_000_000 || channel != global {
		t.Fatalf("budget=%d/%d err=%v", channel, global, err)
	}
	if _, err = svc.Handle(ctx, commands.Authority{ChannelID: c.ID, SessionID: a.LiveSessionID}, cmd); err != nil || calls != 1 {
		t.Fatalf("retry calls=%d err=%v", calls, err)
	}
	entered := make(chan struct{})
	svc.Extractor = extractFunc(func(ctx context.Context, _ []receipt.Segment) (commands.Extraction, error) {
		close(entered)
		<-ctx.Done()
		return commands.Extraction{}, ctx.Err()
	})
	done := make(chan error, 1)
	cmd.ID = "cancel-command"
	cmd.UserID = "another-viewer"
	go func() {
		_, err := svc.Handle(ctx, commands.Authority{ChannelID: c.ID, SessionID: a.LiveSessionID}, cmd)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("extractor did not start")
	}
	if err = manager.Cancel(a); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("canceled extraction succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled extraction hung")
	}
	if _, err = svc.Nearest(ctx, later, 3); !errors.Is(err, runtime.ErrStale) {
		t.Fatalf("stale runtime: %v", err)
	}
}

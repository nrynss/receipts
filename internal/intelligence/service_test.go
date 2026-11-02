package intelligence

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	"receipts/internal/claims"
	"receipts/internal/commands"
	"receipts/internal/detect"
	"receipts/internal/judge"
	"receipts/internal/ports"
	"receipts/internal/receipt"
	"receipts/internal/retrieval"
	runtime "receipts/internal/runtime"
	"receipts/internal/store"
	"receipts/internal/transcribe"
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

type serviceRoundTripper func(*http.Request) (*http.Response, error)

func (f serviceRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mockedLunaHTTP(t *testing.T) (*http.Client, *atomic.Int64, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	var extractionCalls, sensitivityCalls, judgeCalls atomic.Int64
	client := &http.Client{Transport: serviceRoundTripper(func(r *http.Request) (*http.Response, error) {
		respond := func(body []byte) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
		}
		switch r.URL.String() {
		case "https://api.openai.com/v1/chat/completions":
			extractionCalls.Add(1)
			var request struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) != 2 || request.Messages[1].Role != "user" {
				t.Fatalf("invalid mocked Luna extraction request: %v", err)
			}
			var sources []receipt.Segment
			if err := json.Unmarshal([]byte(request.Messages[1].Content), &sources); err != nil || len(sources) == 0 {
				t.Fatalf("invalid mocked extraction sources: %v", err)
			}
			var chosen receipt.Segment
			for _, source := range sources {
				if receipt.IsSourcePromise(source.Text, "I will never play this map.") {
					chosen = source
					break
				}
			}
			if chosen.ID == "" {
				t.Fatal("no complete source in recorded extraction input")
			}
			content, _ := json.Marshal(map[string]any{"eligible": true, "claim": "I will never play this map.", "source_id": chosen.ID, "topic": "promise", "sensitive": false})
			body, _ := json.Marshal(map[string]any{
				"model": claims.LunaModel, "service_tier": "default",
				"usage":   map[string]int{"prompt_tokens": 200, "completion_tokens": 10, "total_tokens": 210},
				"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(content)}}},
			})
			return respond(body)
		case "https://api.openai.com/v1/decisions":
			var request struct {
				Questions []struct {
					Name string `json:"name"`
				} `json:"questions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Questions) != 1 {
				t.Fatalf("invalid mocked Decisions request: %v", err)
			}
			answer := map[string]any{"type": "choice", "confidence": 0.99}
			switch request.Questions[0].Name {
			case "sensitivity":
				sensitivityCalls.Add(1)
				answer["name"], answer["choice"] = "sensitivity", "not_sensitive"
				answer["probabilities"] = []any{
					map[string]any{"value": "sensitive", "probability": 0.005},
					map[string]any{"value": "not_sensitive", "probability": 0.99},
					map[string]any{"value": "unsure", "probability": 0.005},
				}
			case "verdict":
				judgeCalls.Add(1)
				answer["name"], answer["choice"] = "verdict", "contradicts"
				answer["probabilities"] = []any{
					map[string]any{"value": "contradicts", "probability": 0.99},
					map[string]any{"value": "does_not_contradict", "probability": 0.005},
					map[string]any{"value": "unsure", "probability": 0.005},
				}
			default:
				t.Fatalf("unexpected Decisions question %q", request.Questions[0].Name)
			}
			body, _ := json.Marshal(map[string]any{
				"model": judge.DecisionsModel, "answers": []any{answer},
				"usage": map[string]int{"input_tokens": 100, "output_tokens": 5, "total_tokens": 105},
			})
			return respond(body)
		default:
			t.Fatalf("unexpected provider endpoint %s", r.URL)
			return nil, errors.New("unexpected provider endpoint")
		}
	})}
	return client, &extractionCalls, &sensitivityCalls, &judgeCalls
}

func TestCaptureReindexAndRetrieveWithDurableBudget(t *testing.T) {
	for _, prefix := range []string{"", "Blue card. "} {
		t.Run(prefix, func(t *testing.T) { testCaptureReindexAndRetrieve(t, prefix, false) })
	}
	t.Run("overlapping transcription window", func(t *testing.T) { testCaptureReindexAndRetrieve(t, "", true) })
}

func testCaptureReindexAndRetrieve(t *testing.T, prefix string, overlap bool) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "ledger.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sessionStart := time.Now().UTC().Truncate(time.Millisecond).Add(-10 * time.Second)
	var clock atomic.Int64
	clock.Store(sessionStart.UnixMilli())
	ledger, err := store.New(ctx, db, store.Retention{}, func() time.Time { return time.UnixMilli(clock.Load()).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	c, err := ledger.CreateChannel(ctx, "streamer", "streamer")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','streamer','streamer',?,X'00',?,'[]','connected',?)`, c.ID, sessionStart.Add(time.Hour).UnixMilli(), sessionStart.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	activation, err := ledger.StartOwnedLiveSession(ctx, "owner", c.ID, "broadcast", sessionStart, false)
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
	original := receipt.Segment{ID: "original", SessionID: a.LiveSessionID, Start: time.Second, End: 2 * time.Second, Text: prefix + "I will never play this map.", SpeakerID: c.ID, Attribution: "broadcaster", TimingBasis: "coarse_pcm_ingest_hls_delay_unknown"}
	if overlap {
		clock.Store(sessionStart.Add(5 * time.Second).UnixMilli())
		calls := 0
		worker := transcribe.Worker{Authority: a, Meter: manager, Ledger: ledger, Overlap: true, SoleBroadcaster: true, NowOffset: func() time.Duration { return 5 * time.Second }, Transcriber: transcriptionFunc(func(context.Context, []byte) (string, error) {
			calls++
			if calls == 1 {
				return "I will never", nil
			}
			return original.Text, nil
		})}
		for i := 0; i < 2; i++ {
			if err = worker.Submit(ctx, transcribe.Chunk{Sequence: uint64(i + 1), Start: time.Duration(i+1) * time.Second, End: time.Duration(i+2) * time.Second, WAV: oneSecondPCM()}); err != nil {
				t.Fatal(err)
			}
		}
		sources, e := ledger.Transcript(ctx, a.LiveSessionID)
		if e != nil {
			t.Fatal(e)
		}
		if len(sources) != 2 {
			t.Fatalf("overlap sources %#v", sources)
		}
		for _, source := range sources {
			if source.End == 3*time.Second {
				original = source
			}
		}
		if original.Start != time.Second || original.End != 3*time.Second {
			t.Fatal("window lost raw source bounds")
		}
	} else if err = ledger.AddLiveSegment(ctx, a.AccountID, a.ChannelID, a.LiveSessionID, a.Generation, original); err != nil {
		t.Fatal(err)
	}
	captureTime := sessionStart.Add(10 * time.Second)
	clock.Store(captureTime.UnixMilli())
	provider, extractionCalls, sensitivityCalls, decisionsJudgeCalls := mockedLunaHTTP(t)
	apiKey := "offline-test-key"
	svc := &Service{
		Authority: a, Meter: manager, Ledger: ledger, Index: ledger,
		Space:           retrieval.Space{Model: retrieval.DefaultModel, Version: "v1", Dimensions: 2},
		ExtractionModel: claims.LunaModel,
		Extractor:       claims.Client{APIKey: apiKey, Model: claims.LunaModel, HTTP: provider},
		Sensitive:       claims.DecisionsClient{APIKey: apiKey, HTTP: provider},
		Judge:           judge.DecisionsClient{APIKey: apiKey, HTTP: provider},
		Embedder:        embedFunc(func(context.Context, string) ([]float64, error) { return []float64{1, 0}, nil }),
	}
	cmd := ports.ChatCommand{ID: "command", UserID: "viewer", Login: "viewer", Command: "!receipt", ReceivedAt: captureTime}
	result, err := svc.Handle(ctx, commands.Authority{ChannelID: c.ID, SessionID: a.LiveSessionID}, cmd)
	if err != nil || result.Code != commands.ReplyCaptured || sensitivityCalls.Load() != 2 || extractionCalls.Load() != 1 {
		t.Fatalf("capture=%#v sensitivity calls=%d extraction calls=%d err=%v", result, sensitivityCalls.Load(), extractionCalls.Load(), err)
	}
	evidence, err := ledger.Evidence(ctx, result.Receipt.ID)
	if err != nil || len(evidence) != 1 || evidence[0].Text != original.Text || evidence[0].TimingBasis != "coarse_pcm_ingest_hls_delay_unknown" {
		t.Fatalf("provenance=%#v %v", evidence, err)
	}
	if err = svc.Reindex(ctx); err != nil {
		t.Fatal(err)
	}
	clock.Store(sessionStart.Add(15 * time.Second).UnixMilli())
	later := receipt.Segment{ID: "later", SessionID: a.LiveSessionID, Start: 11 * time.Second, End: 12 * time.Second, Text: "I will play this map.", SpeakerID: c.ID, Attribution: "broadcaster", TimingBasis: "coarse_pcm_ingest_hls_delay_unknown"}
	if err = ledger.AddLiveSegment(ctx, a.AccountID, a.ChannelID, a.LiveSessionID, a.Generation, later); err != nil {
		t.Fatal(err)
	}
	matches, err := svc.Nearest(ctx, later, 3)
	if err != nil || len(matches) != 1 || matches[0].Receipt.ID != result.Receipt.ID {
		t.Fatalf("matches=%#v err=%v", matches, err)
	}
	svc.DetectionStore = ledger
	earlier := receipt.Segment{ID: "earlier", SessionID: a.LiveSessionID, Start: 3 * time.Second, End: 4 * time.Second, Text: "Earlier words.", SpeakerID: c.ID, Attribution: "broadcaster", TimingBasis: "coarse_pcm_ingest_hls_delay_unknown"}
	if err = ledger.AddLiveSegment(ctx, a.AccountID, a.ChannelID, a.LiveSessionID, a.Generation, earlier); err != nil {
		t.Fatal(err)
	}
	for _, src := range []receipt.Segment{original, earlier} {
		if err = svc.JudgeSource(ctx, src); err != nil {
			t.Fatal(err)
		}
	}
	if decisionsJudgeCalls.Load() != 0 {
		t.Fatalf("paid Decisions calls for own or earlier source: %d", decisionsJudgeCalls.Load())
	}
	if err = svc.JudgeSource(ctx, later); err != nil {
		t.Fatalf("judge source: %v", err)
	}
	if decisionsJudgeCalls.Load() != 1 {
		t.Fatalf("Decisions judge calls=%d, want one", decisionsJudgeCalls.Load())
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
	if _, err = svc.Handle(ctx, commands.Authority{ChannelID: c.ID, SessionID: a.LiveSessionID}, cmd); err != nil || extractionCalls.Load() != 1 {
		t.Fatalf("retry extraction calls=%d err=%v", extractionCalls.Load(), err)
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

type transcriptionFunc func(context.Context, []byte) (string, error)

func (f transcriptionFunc) Transcribe(ctx context.Context, b []byte) (string, error) {
	return f(ctx, b)
}
func oneSecondPCM() []byte {
	b := make([]byte, 44+32000)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], 32000)
	return b
}

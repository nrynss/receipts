package detect

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/api"
	"receipts/internal/auth"
	"receipts/internal/events"
	"receipts/internal/ports"
	"receipts/internal/receipt"
	"receipts/internal/replay"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) Add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }

type harness struct {
	db     *sqlite.DB
	ledger *store.Store
	clock  *testClock
	mgr    *channelruntime.Manager
	a      auth.ChannelContext
	r      receipt.Receipt
	svc    Service
	path   string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	h := &harness{clock: &testClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, path: filepath.Join(t.TempDir(), "ledger.db")}
	var err error
	h.db, err = sqlite.Open(ctx, sqlite.Config{Path: h.path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.db.Close() })
	h.ledger, err = store.NewWithIDSource(ctx, h.db, store.Retention{}, h.clock.Now, replay.IDs())
	if err != nil {
		t.Fatal(err)
	}
	c, err := h.ledger.CreateChannel(ctx, "streamer", "streamer")
	if err != nil {
		t.Fatal(err)
	}
	session, err := h.ledger.StartSession(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','streamer','streamer',?,X'00',?,'[]','connected',?)`, c.ID, h.clock.Now().Add(time.Hour).UnixMilli(), h.clock.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	h.a = auth.ChannelContext{AccountID: "owner", ChannelID: c.ID, LiveSessionID: session.ID, Generation: 1}
	h.mgr = channelruntime.New(channelruntime.Config{MaxChannels: 4})
	if _, err = h.mgr.StartVerified(ctx, h.a, h.ledger); err != nil {
		t.Fatal(err)
	}
	original := h.add(t, "original", "I will never take the shortcut.")
	h.r = h.makeReceipt(t, original, "Never take the shortcut")
	h.svc = Service{Store: h.ledger, Runtime: h.mgr, Sensitive: safe{}, Now: h.clock.Now, Judge: judgeFunc(func(context.Context, receipt.Receipt, receipt.Segment) (Verdict, error) { return Contradicts, nil })}
	return h
}
func (h *harness) add(t *testing.T, id, text string) receipt.Segment {
	t.Helper()
	v := receipt.Segment{ID: id, SessionID: h.a.LiveSessionID, Text: text, Attribution: "broadcaster", SpeakerID: "streamer"}
	if err := h.ledger.AddSegment(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	return v
}
func (h *harness) makeReceipt(t *testing.T, s receipt.Segment, claim string) receipt.Receipt {
	t.Helper()
	r, err := h.ledger.CreateReceipt(context.Background(), receipt.ReceiptInput{ChannelID: h.a.ChannelID, SessionID: h.a.LiveSessionID, Claim: claim, ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{{SourceID: s.ID, Text: s.Text, Attribution: s.Attribution, Start: s.Start, End: s.End}}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestBlockedJudgeRechecksState(t *testing.T) {
	for _, mode := range []string{"void", "expiry", "pause", "rollover", "revoke", "cancel", "source"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			source := h.add(t, "later", "I will take the shortcut.")
			entered := make(chan struct{})
			release := make(chan struct{})
			h.svc.Judge = judgeFunc(func(context.Context, receipt.Receipt, receipt.Segment) (Verdict, error) {
				close(entered)
				<-release
				return Contradicts, nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- h.svc.Check(ctx, h.a, h.r.ID, source) }()
			<-entered
			var err error
			switch mode {
			case "void":
				err = h.ledger.Void(ctx, h.a.ChannelID, h.r.ID)
			case "expiry":
				_, err = h.db.Writer().ExecContext(ctx, `UPDATE receipts SET expires_at=? WHERE id=?`, h.clock.Now().UnixMilli(), h.r.ID)
			case "pause":
				err = h.mgr.SetPaused(h.a, true)
			case "rollover":
				_, err = h.ledger.StartSession(ctx, h.a.ChannelID)
			case "revoke":
				_, err = h.db.Writer().ExecContext(ctx, `UPDATE accounts SET status='reconnect_required' WHERE id=?`, h.a.AccountID)
				if err == nil {
					h.mgr.RevokeAccount(h.a.AccountID)
				}
			case "cancel":
				cancel()
			case "source":
				_, err = h.db.Writer().ExecContext(ctx, `UPDATE transcript_segments SET text='changed' WHERE id=?`, source.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			close(release)
			if err = <-done; err == nil {
				t.Fatal("delayed decision accepted")
			}
			if current, e := h.ledger.CurrentStamp(context.Background(), h.a.ChannelID, h.a.LiveSessionID); e != nil || current != nil {
				t.Fatalf("unexpected stamp %v %v", current, e)
			}
		})
	}
}
func TestSourcePreflightAvoidsJudge(t *testing.T) {
	h := newHarness(t)
	src := h.add(t, "later", "I will take the shortcut.")
	calls := 0
	h.svc.Judge = judgeFunc(func(context.Context, receipt.Receipt, receipt.Segment) (Verdict, error) {
		calls++
		return Contradicts, nil
	})
	for _, change := range []func(*receipt.Segment){func(s *receipt.Segment) { s.Text = "forged" }, func(s *receipt.Segment) { s.End = time.Second }, func(s *receipt.Segment) { s.SpeakerID = "other" }, func(s *receipt.Segment) { s.SessionID = "foreign" }} {
		v := src
		change(&v)
		if err := h.svc.Check(context.Background(), h.a, h.r.ID, v); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
	if calls != 0 {
		t.Fatalf("judge called %d times", calls)
	}
}
func TestCooldownSurvivesVoidAndRestart(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	s1 := h.add(t, "one", "I take it.")
	s2 := h.add(t, "two", "I take it again.")
	r2 := h.makeReceipt(t, s1, "Never take it twice")
	if err := h.svc.Check(ctx, h.a, h.r.ID, s1); err != nil {
		t.Fatal(err)
	}
	if err := h.ledger.Void(ctx, h.a.ChannelID, h.r.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.mgr.Stop(h.a); err != nil {
		t.Fatal(err)
	}
	if err := h.db.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	h.db, err = sqlite.Open(ctx, sqlite.Config{Path: h.path})
	if err != nil {
		t.Fatal(err)
	}
	h.ledger, err = store.New(ctx, h.db, store.Retention{}, h.clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	h.mgr = channelruntime.New(channelruntime.Config{})
	h.a.Generation++
	if _, err = h.mgr.StartVerified(ctx, h.a, h.ledger); err != nil {
		t.Fatal(err)
	}
	h.svc.Store = h.ledger
	h.svc.Runtime = h.mgr
	if err = h.svc.Check(ctx, h.a, r2.ID, s2); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("cooldown after restart: %v", err)
	}
	h.clock.Add(30 * time.Second)
	if err = h.svc.Check(ctx, h.a, r2.ID, s2); err != nil {
		t.Fatalf("boundary: %v", err)
	}
}
func TestConcurrentCandidatesShareCooldown(t *testing.T) {
	h := newHarness(t)
	s := h.add(t, "later", "I take the shortcut.")
	r2 := h.makeReceipt(t, s, "Never take that turn")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{h.r.ID, r2.ID} {
		wg.Add(1)
		go func(id string) { defer wg.Done(); results <- h.svc.Check(context.Background(), h.a, id, s) }(id)
	}
	wg.Wait()
	close(results)
	n := 0
	for err := range results {
		if err == nil {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("successful stamps=%d", n)
	}
}
func TestChannelIsolation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	c, err := h.ledger.CreateChannel(ctx, "other", "other")
	if err != nil {
		t.Fatal(err)
	}
	session, err := h.ledger.StartSession(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('other','twitch','other','other',?,X'00',?,'[]','connected',?)`, c.ID, h.clock.Now().Add(time.Hour).UnixMilli(), h.clock.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	a := auth.ChannelContext{AccountID: "other", ChannelID: c.ID, LiveSessionID: session.ID, Generation: 1}
	ch, err := h.mgr.StartVerified(ctx, a, h.ledger)
	if err != nil {
		t.Fatal(err)
	}
	src := receipt.Segment{ID: "other-source", SessionID: session.ID, Text: "I'll take it.", Attribution: "broadcaster"}
	if err = h.ledger.AddSegment(ctx, src); err != nil {
		t.Fatal(err)
	}
	if err = h.svc.Check(ctx, a, h.r.ID, src); !errors.Is(err, store.ErrNotEligible) {
		t.Fatalf("foreign receipt: %v", err)
	}
	if ch.Events.Sequence() != 0 {
		t.Fatal("foreign stamp")
	}
	if _, err = h.ledger.PendingReviews(ctx, a.AccountID, h.a.ChannelID, 100); err != nil {
		t.Fatal(err)
	}
	if values, err := h.ledger.PendingReviews(ctx, a.AccountID, h.a.ChannelID, 100); err != nil || len(values) != 0 {
		t.Fatalf("foreign reviews: %v %v", values, err)
	}
}

type transcriptCheck struct{ h *harness }

func (s transcriptCheck) AcceptTranscript(ctx context.Context, v ports.TranscriptSegment) error {
	src := receipt.Segment{ID: v.ID, SessionID: v.SessionID, Text: v.Text, Start: v.Start, End: v.End, Attribution: string(v.Attribution), SpeakerID: v.SpeakerID}
	if err := s.h.ledger.AddSegment(ctx, src); err != nil {
		return err
	}
	return s.h.svc.Check(ctx, s.h.a, s.h.r.ID, src)
}

type snapshotReader struct{ h *harness }

func (r snapshotReader) OverlaySnapshot(req *http.Request) (events.OverlaySnapshot, error) {
	v, err := r.h.ledger.CurrentStamp(req.Context(), r.h.a.ChannelID, r.h.a.LiveSessionID)
	return events.OverlaySnapshot{Channel: r.h.a.ChannelID, Live: true, CurrentStamp: v}, err
}
func (snapshotReader) ReviewQueue(*http.Request) ([]events.ReviewItem, error) { return nil, nil }
func (snapshotReader) Health(*http.Request) (events.Health, error)            { return events.Health{}, nil }
func TestReplaySSEAndSnapshotRecovery(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	ch, err := h.mgr.Resolve(h.a)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: memoryOverlayTransport{handler: api.New(api.Config{OverlayCredential: "overlay", ControlCredential: "control", Reader: snapshotReader{h}, Events: ch.Events})}}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://overlay.test/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer overlay")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if scanner.Text() == "" {
			break
		}
	}
	at := h.clock.Now()
	file, err := os.Open("../../testdata/replay/t22-detection.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	f, err := replay.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	clock := replay.NewClock(at)
	if err = f.Run(ctx, replay.Sinks{Transcript: transcriptCheck{h}}, clock); err != nil {
		t.Fatal(err)
	}
	var frame strings.Builder
	for scanner.Scan() {
		if scanner.Text() == "" {
			break
		}
		frame.WriteString(scanner.Text())
		frame.WriteByte('\n')
	}
	if !strings.Contains(frame.String(), "event: stamp") || !strings.Contains(frame.String(), `"sequence":1`) || !strings.Contains(frame.String(), at.Format(time.RFC3339)) {
		t.Fatalf("stamp frame %q", frame.String())
	}
	req, err = http.NewRequest("GET", "http://overlay.test/api/overlay", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer overlay")
	snapshotResp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotResp.Body.Close()
	var snapshot events.OverlaySnapshot
	if err = json.NewDecoder(snapshotResp.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Sequence != 1 || snapshot.CurrentStamp == nil || snapshot.CurrentStamp.ID != h.r.ID || snapshot.CurrentStamp.ClippedBy != "viewer" {
		t.Fatalf("snapshot %+v", snapshot)
	}
	// Reconnect from the received transport ID: the server sends reset, and
	// the authoritative snapshot has already incorporated sequence 1.
	reconnectCtx, stopReconnect := context.WithTimeout(ctx, 150*time.Millisecond)
	defer stopReconnect()
	reconnectReq, err := http.NewRequestWithContext(reconnectCtx, "GET", "http://overlay.test/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	reconnectReq.Header.Set("Authorization", "Bearer overlay")
	reconnectReq.Header.Set("Last-Event-ID", "1")
	reconnect, err := client.Do(reconnectReq)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnect.Body.Close()
	lines := bufio.NewScanner(reconnect.Body)
	var rest strings.Builder
	for lines.Scan() {
		rest.WriteString(lines.Text())
		rest.WriteByte('\n')
	}
	if strings.Contains(rest.String(), "event: stamp") || !strings.Contains(rest.String(), "event: reset") {
		t.Fatalf("reconnect frames %q", rest.String())
	}
	if err = h.svc.Check(ctx, h.a, h.r.ID, receipt.Segment{ID: "replayed", SessionID: h.a.LiveSessionID, Text: "Fine, I'll take the shortcut.", Attribution: "broadcaster", SpeakerID: "streamer"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("replay duplicate %v", err)
	}
	if ch.Events.Sequence() != 1 {
		t.Fatal("duplicate emitted")
	}
	// A fresh runtime has a new epoch and no replayed event, while its snapshot
	// still contains the stamp as state for a reconnecting overlay.
	if err = h.mgr.Stop(h.a); err != nil {
		t.Fatal(err)
	}
	h.a.Generation++
	resumed, err := h.mgr.StartVerified(ctx, h.a, h.ledger)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := resumed.Events.Snapshot(func() (events.OverlaySnapshot, error) {
		return snapshotReader{h}.OverlaySnapshot(httptest.NewRequest("GET", "/api/overlay", nil))
	})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Sequence != 0 || restored.CurrentStamp == nil || restored.CurrentStamp.ID != h.r.ID || restored.Epoch == snapshot.Epoch {
		t.Fatalf("restart snapshot %+v", restored)
	}

}

// The offline replay acceptance exercises the real HTTP handler and SSE
// framing through an in-memory pipe, without needing any listener or network.
type memoryOverlayTransport struct{ handler http.Handler }
type memoryStreamWriter struct {
	header http.Header
	writer *io.PipeWriter
}

func (w *memoryStreamWriter) Header() http.Header         { return w.header }
func (w *memoryStreamWriter) WriteHeader(int)             {}
func (w *memoryStreamWriter) Write(p []byte) (int, error) { return w.writer.Write(p) }
func (w *memoryStreamWriter) Flush()                      {}

type memoryStreamBody struct {
	*io.PipeReader
	cancel context.CancelFunc
}

func (b memoryStreamBody) Close() error { b.cancel(); return b.PipeReader.Close() }
func (t memoryOverlayTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path != "/api/events" {
		recorder := httptest.NewRecorder()
		t.handler.ServeHTTP(recorder, r)
		return recorder.Result(), nil
	}
	ctx, cancel := context.WithCancel(r.Context())
	reader, writer := io.Pipe()
	stop := context.AfterFunc(ctx, func() { _ = writer.CloseWithError(ctx.Err()) })
	go func() {
		defer stop()
		defer writer.Close()
		t.handler.ServeHTTP(&memoryStreamWriter{header: make(http.Header), writer: writer}, r.WithContext(ctx))
	}()
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: memoryStreamBody{PipeReader: reader, cancel: cancel}, Request: r}, nil
}

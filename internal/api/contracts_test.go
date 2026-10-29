package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"receipts/internal/events"
	"receipts/internal/ports"
)

type fixtureReader struct{}

func (fixtureReader) OverlaySnapshot(*http.Request) (events.OverlaySnapshot, error) {
	return events.OverlaySnapshot{Epoch: "test-epoch", Channel: "demo", Live: true, Sequence: 7, Receipts: []events.PublicReceipt{{ID: "r1", Claim: "Will try the route", ClipURL: "https://clips.example/r1", ClipStatus: "ready", Verdict: "undecided", CreatedAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}}}, nil
}
func (fixtureReader) ReviewQueue(*http.Request) ([]events.ReviewItem, error) {
	return []events.ReviewItem{{ID: "review1", Claim: "Will try the route", Reason: "speaker uncertain", CreatedAt: time.Date(2000, 1, 1, 0, 0, 1, 0, time.UTC)}}, nil
}
func (fixtureReader) Health(*http.Request) (events.Health, error) {
	at := time.Date(2000, 1, 1, 0, 0, 1, 0, time.UTC)
	return events.Health{Status: "ok", Mode: "fixture", LastEventAt: &at}, nil
}

func TestGoPayloadsMatchGoldenContracts(t *testing.T) {
	stamp := time.Date(2000, 1, 1, 0, 0, 1, 0, time.UTC)
	receipt := events.PublicReceipt{ID: "r1", Claim: "Will try the route", ClipURL: "https://clips.example/r1", ClipStatus: "ready", Verdict: "undecided", CreatedAt: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)}
	tests := map[string]any{
		"overlay.snapshot.json":         fixtureReader{}.snapshot(),
		"overlay.stamp.json":            events.OverlayEvent{Epoch: "test-epoch", Sequence: 8, OccurredAt: stamp, Receipt: &receipt},
		"review.queue.json":             []events.ReviewItem{{ID: "review1", Claim: "Will try the route", Reason: "speaker uncertain", CreatedAt: stamp}},
		"health.json":                   events.Health{Status: "ok", Mode: "fixture", LastEventAt: &stamp},
		"transcript.segment.json":       ports.TranscriptSegment{ID: "segment1", SessionID: "session1", SpeakerID: "streamer", Attribution: ports.AttributionBroadcaster, Start: time.Second, End: 2 * time.Second, Text: "Will try the route"},
		"chat.command.json":             ports.ChatCommand{ID: "message1", ChannelID: "channel1", SessionID: "session1", UserID: "viewer1", Login: "viewer", Role: ports.ChatViewer, Command: "!receipt", Arguments: "that promise", ReceivedAt: stamp},
		"clip.result.json":              ports.ClipResult{RequestID: "request1", ClipID: "clip1", URL: "https://clips.example/clip1", CreatedAt: stamp, Status: ports.ClipReady},
		"live-session.change.json":      ports.LiveSessionChange{SessionID: "session1", ChannelID: "channel1", State: "live", ObservedAt: stamp, Confirmed: true},
		"control.pause.json":            ControlInput{Paused: boolPointer(true)},
		"control.void.json":             ControlInput{ReceiptID: "r1", Note: "wrong claim"},
		"control.review.json":           ControlInput{ReviewID: "q1", ReviewDecision: ReviewApprove},
		"control.receipt-decision.json": ControlInput{ReceiptID: "r1", ReceiptDecision: DecisionContextChanged, Note: "context"},
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile("../../testdata/contracts/" + name)
			if err != nil {
				t.Fatal(err)
			}
			decoded := reflect.New(reflect.TypeOf(value))
			if err := json.Unmarshal(want, decoded.Interface()); err != nil {
				t.Fatalf("decode golden: %v", err)
			}
			got, err = json.Marshal(decoded.Elem().Interface())
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
				t.Fatalf("payload mismatch\n got %s\nwant %s", got, want)
			}
		})
	}
}
func boolPointer(value bool) *bool { return &value }
func (r fixtureReader) snapshot() events.OverlaySnapshot {
	x, _ := r.OverlaySnapshot(httptest.NewRequest("GET", "/", nil))
	return x
}

func TestOverlayAndControlAuthorizationBoundaries(t *testing.T) {
	s := New(Config{OverlayCredential: "read-secret", ControlCredential: "write-secret", Reader: fixtureReader{}})
	cases := []struct {
		name, path, method, auth, query string
		status                          int
	}{
		{"overlay query denied", "/api/overlay?token=read-secret", "GET", "", "", 401},
		{"overlay bearer read", "/api/overlay", "GET", "Bearer read-secret", "", 200},
		{"review denied to overlay", "/api/review", "GET", "Bearer read-secret", "", 401},
		{"control denied to overlay", "/api/control/pause", "POST", "Bearer read-secret", `{"paused":true}`, 401},
		{"control credential is not overlay", "/api/overlay?token=write-secret", "GET", "", "", 401},
		{"control cannot use query", "/api/control/pause?token=write-secret", "POST", "", `{"paused":true}`, 401},
		{"unimplemented write not success", "/api/control/pause", "POST", "Bearer write-secret", `{"paused":true}`, 501},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.query))
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			res := httptest.NewRecorder()
			s.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("status=%d want %d: %s", res.Code, tc.status, res.Body.String())
			}
			if tc.status == http.StatusUnauthorized && !strings.Contains(res.Body.String(), `"error":{"code":"unauthorized"`) {
				t.Fatalf("error was not Keel envelope: %s", res.Body.String())
			}
		})
	}
}

func TestEqualCredentialsFailClosed(t *testing.T) {
	s := New(Config{OverlayCredential: "same", ControlCredential: "same", Reader: fixtureReader{}})
	for _, path := range []string{"/api/overlay", "/api/review", "/api/health"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer same")
		res := httptest.NewRecorder()
		s.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("equal credentials accepted at %s: %d", path, res.Code)
		}
	}
}

type recordingControls struct {
	review  ReviewDecision
	receipt ReceiptDecision
}

func (*recordingControls) SetPaused(*http.Request, ControlInput) error   { return nil }
func (*recordingControls) VoidReceipt(*http.Request, ControlInput) error { return nil }
func (c *recordingControls) ResolveReview(_ *http.Request, in ControlInput) error {
	c.review = in.ReviewDecision
	return nil
}
func (c *recordingControls) SetReceiptDecision(_ *http.Request, in ControlInput) error {
	c.receipt = in.ReceiptDecision
	return nil
}

func TestTypedStreamerAndReviewIntents(t *testing.T) {
	controls := &recordingControls{}
	s := New(Config{OverlayCredential: "read", ControlCredential: "write", Controls: controls})
	cases := []struct {
		path, body  string
		status      int
		wantReview  ReviewDecision
		wantReceipt ReceiptDecision
	}{
		{"/api/control/review", `{"review_id":"q1","decision":"approve"}`, http.StatusNoContent, ReviewApprove, ""},
		{"/api/control/review", `{"review_id":"q1","decision":"reject"}`, http.StatusNoContent, ReviewReject, ""},
		{"/api/control/decision", `{"receipt_id":"r1","receipt_decision":"admit"}`, http.StatusNoContent, "", DecisionAdmit},
		{"/api/control/decision", `{"receipt_id":"r1","receipt_decision":"appeal"}`, http.StatusNoContent, "", DecisionAppeal},
		{"/api/control/decision", `{"receipt_id":"r1","receipt_decision":"context_changed"}`, http.StatusNoContent, "", DecisionContextChanged},
		{"/api/control/decision", `{"receipt_id":"r1","receipt_decision":"void"}`, http.StatusBadRequest, "", ""},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer write")
		res := httptest.NewRecorder()
		s.ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("%s returned %d, want %d: %s", tc.body, res.Code, tc.status, res.Body.String())
		}
		if tc.wantReview != "" && controls.review != tc.wantReview {
			t.Fatalf("review intent=%q want %q", controls.review, tc.wantReview)
		}
		if tc.wantReceipt != "" && controls.receipt != tc.wantReceipt {
			t.Fatalf("receipt intent=%q want %q", controls.receipt, tc.wantReceipt)
		}
	}
}

type publishOnFlush struct {
	*httptest.ResponseRecorder
	once    bool
	publish func()
}

func (w *publishOnFlush) Flush() {
	if !w.once {
		w.once = true
		w.publish()
	}
	w.ResponseRecorder.Flush()
}

func TestStreamRegistersBeforeResetReadinessAndDoesNotMissConcurrentPublish(t *testing.T) {
	publisher := events.NewPublisher("epoch")
	s := New(Config{OverlayCredential: "read", ControlCredential: "write", Events: publisher})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest("GET", "/api/events", nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer read")
	writer := &publishOnFlush{ResponseRecorder: httptest.NewRecorder(), publish: func() {
		publisher.Commit(events.Stamp, func() events.OverlayEvent { return events.OverlayEvent{OccurredAt: time.Unix(1, 0)} })
	}}
	done := make(chan struct{})
	go func() { s.ServeHTTP(writer, request); close(done) }()
	select {
	case <-done:
		t.Fatal("stream ended before a concurrent publish")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	<-done
	body := writer.Body.String()
	if !strings.Contains(body, "event: reset\nid: 0\ndata: {}") {
		t.Fatalf("missing Chaaya reset frame: %s", body)
	}
	if !strings.Contains(body, "event: stamp") {
		t.Fatalf("event published at stream readiness was missed: %s", body)
	}
	if strings.Index(body, "event: reset") > strings.Index(body, "event: stamp") {
		t.Fatalf("reset did not precede first event: %s", body)
	}
}

func TestReadOnlyOverlayCredentialCannotSubscribeToControls(t *testing.T) {
	s := New(Config{OverlayCredential: "read", ControlCredential: "write", Reader: fixtureReader{}})
	for _, path := range []string{"/api/review", "/api/health"} {
		req := httptest.NewRequest("GET", path+"?token=read", nil)
		res := httptest.NewRecorder()
		s.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s accepted overlay credential: %d", path, res.Code)
		}
	}
}

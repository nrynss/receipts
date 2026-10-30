// Package demo runs the public recorded walkthrough in visitor-isolated,
// disposable ledgers. It contains no live provider adapters.
package demo

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"receipts/internal/appeal"
	"receipts/internal/auth"
	"receipts/internal/commands"
	"receipts/internal/detect"
	"receipts/internal/lore"
	"receipts/internal/ports"
	"receipts/internal/receipt"
	"receipts/internal/replay"
	runtime "receipts/internal/runtime"
)

//go:embed fixtures/*.json
var recorded embed.FS

const cookieName = "receipts_demo"
const visitorTTL = 20 * time.Minute

type Result struct {
	Stage      string   `json:"stage"`
	Claim      string   `json:"claim,omitempty"`
	Verdict    string   `json:"verdict,omitempty"`
	ClipStatus string   `json:"clip_status,omitempty"`
	Lore       string   `json:"lore,omitempty"`
	Events     []string `json:"events,omitempty"`
}
type visitor struct {
	mu       sync.Mutex
	created  atomic.Int64
	path     string
	ledger   *replay.Ledger
	clock    *replay.Clock
	result   Result
	requests int
	refs     int
}
type Server struct {
	mu       sync.Mutex
	visitors map[[32]byte]*visitor
	root     string
	max      int
	now      func() time.Time
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	closed   bool
	requests sync.WaitGroup
}

func New(root string, maxVisitors int) *Server {
	if maxVisitors <= 0 {
		maxVisitors = 64
	}
	if root == "" {
		root = os.TempDir()
	}
	s := &Server{visitors: map[[32]byte]*visitor{}, root: root, max: maxVisitors, now: time.Now, stop: make(chan struct{}), done: make(chan struct{})}
	go s.cleanupLoop()
	return s
}

// Close disposes every ephemeral visitor database when the server shuts down.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.done
	s.requests.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	var first error
	for k, v := range s.visitors {
		if err := s.dispose(v); err != nil && first == nil {
			first = err
		}
		delete(s.visitors, k)
	}
	return first
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/demo/api/") {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path != "/demo/api/replay" && r.URL.Path != "/demo/api/reset" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost || r.ContentLength != 0 || len(r.TransferEncoding) > 0 || !s.sameOrigin(r) {
		http.Error(w, "method not allowed", 405)
		return
	}
	v, err := s.acquireVisitor(w, r)
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	defer s.release(v)
	v.requests++
	if v.requests > 30 {
		http.Error(w, "demo request limit reached", http.StatusTooManyRequests)
		return
	}
	switch r.URL.Path {
	case "/demo/api/replay":
		if v.ledger == nil {
			if err = s.recreate(v); err != nil {
				http.Error(w, "demo session unavailable", 500)
				return
			}
		}
		if v.result.Stage == "complete" {
			writeJSON(w, v.result)
			return
		}
		replayCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		result, e := s.runReplay(replayCtx, v)
		cancel()
		if e != nil {
			_ = s.recreate(v)
			v.result = Result{Stage: "ready"}
			http.Error(w, "recorded demo could not be replayed", 500)
			return
		}
		v.result = result
		writeJSON(w, result)
	case "/demo/api/reset":
		v.result = Result{Stage: "ready"}
		if err = s.recreate(v); err != nil {
			http.Error(w, "demo reset unavailable", 500)
			return
		}
		v.result = Result{Stage: "ready"}
		writeJSON(w, v.result)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func (s *Server) cleanupLoop() {
	defer close(s.done)
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-tick.C:
			s.expire()
		}
	}
}
func (s *Server) expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, v := range s.visitors {
		created := time.Unix(0, v.created.Load())
		if now.Sub(created) > visitorTTL {
			_ = s.dispose(v)
			delete(s.visitors, k)
		}
	}
}
func (s *Server) acquireVisitor(w http.ResponseWriter, r *http.Request) (*visitor, error) {
	var token string
	if c, e := r.Cookie(cookieName); e == nil {
		token = c.Value
	}
	var key [32]byte
	if len(token) == 64 {
		raw, e := hex.DecodeString(token)
		if e == nil && len(raw) == 32 {
			key = sha256.Sum256(raw)
		}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("demo server is shutting down")
	}
	now := s.now()
	for k, v := range s.visitors {
		if v.refs == 0 && now.Sub(time.Unix(0, v.created.Load())) > visitorTTL {
			_ = s.dispose(v)
			delete(s.visitors, k)
		}
	}
	if key != ([32]byte{}) {
		if v := s.visitors[key]; v != nil {
			v.refs++
			s.requests.Add(1)
			s.mu.Unlock()
			v.mu.Lock()
			return v, nil
		}
	}
	if len(s.visitors) >= s.max {
		s.mu.Unlock()
		return nil, errors.New("demo capacity is full; try again shortly")
	}
	bytes := make([]byte, 32)
	if _, e := rand.Read(bytes); e != nil {
		s.mu.Unlock()
		return nil, e
	}
	token = hex.EncodeToString(bytes)
	key = sha256.Sum256(bytes)
	v := &visitor{result: Result{Stage: "ready"}}
	v.created.Store(now.UnixNano())
	if e := s.recreate(v); e != nil {
		s.mu.Unlock()
		return nil, e
	}
	v.refs = 1
	s.visitors[key] = v
	s.requests.Add(1)
	s.mu.Unlock()
	v.mu.Lock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/demo", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: int(visitorTTL.Seconds())})
	return v, nil
}

func (s *Server) release(v *visitor) {
	v.mu.Unlock()
	s.mu.Lock()
	v.refs--
	s.mu.Unlock()
	s.requests.Done()
}
func (s *Server) recreate(v *visitor) error {
	_ = s.dispose(v)
	dir, e := os.MkdirTemp(s.root, "receipts-demo-")
	if e != nil {
		return e
	}
	clock := replay.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l, e := replay.OpenLedger(context.Background(), filepath.Join(dir, "ledger.db"), clock)
	if e != nil {
		_ = os.RemoveAll(dir)
		return e
	}
	v.path = dir
	v.ledger = l
	v.clock = clock
	return nil
}
func (s *Server) dispose(v *visitor) error {
	if v.ledger != nil {
		e := v.ledger.Close()
		v.ledger = nil
		_ = os.RemoveAll(v.path)
		v.path = ""
		return e
	}
	return nil
}

type extractor struct{}

func (extractor) Extract(context.Context, []receipt.Segment) (commands.Extraction, error) {
	return commands.Extraction{Claim: "Never take the shortcut again.", SourceIDs: []string{"segment-receipt-1"}}, nil
}

type safe struct{}

func (safe) Sensitive(context.Context, string) (bool, error) { return false, nil }

type clipFail struct{}

func (clipFail) Enqueue(context.Context, string, string, string, time.Time) error {
	return errors.New("clips disabled in recorded demo")
}

type transcriptSink func(context.Context, ports.TranscriptSegment) error

func (f transcriptSink) AcceptTranscript(c context.Context, v ports.TranscriptSegment) error {
	return f(c, v)
}

type clipSink func(context.Context, ports.ClipResult) error

func (f clipSink) AcceptClipResult(c context.Context, v ports.ClipResult) error { return f(c, v) }

type chatSink func(context.Context, ports.ChatCommand) error

func (f chatSink) AcceptChatCommand(c context.Context, v ports.ChatCommand) error { return f(c, v) }

func (s *Server) runReplay(ctx context.Context, v *visitor) (Result, error) {
	clock := v.clock
	l := v.ledger
	st := l.Store
	channel, session := l.Channel, l.Session
	if err := l.SeedOwnerAccount(ctx); err != nil {
		return Result{}, err
	}
	// These deterministic IDs only exist in this fresh visitor ledger. The client never selects the authority.
	a := auth.ChannelContext{AccountID: "fixture-owner", ChannelID: channel.ID, LiveSessionID: session.ID, Generation: 1}
	manager := runtime.New(runtime.Config{MaxChannels: 1})
	if _, e := manager.StartVerified(ctx, a, st); e != nil {
		return Result{}, e
	}
	defer manager.Stop(a)
	capService := commands.Service{Ledger: st, Extractor: extractor{}, Sensitive: safe{}, Clips: clipFail{}, Now: clock.Now}
	var captured *receipt.Receipt
	capF, e := openFixture("../../testdata/replay/t21-capture.json")
	if e != nil {
		return Result{}, e
	}
	capF.Inputs[0].Transcript.SessionID = session.ID
	capF.Inputs[1].Chat.ChannelID = channel.ID
	capF.Inputs[1].Chat.SessionID = session.ID
	transcript := transcriptSink(func(c context.Context, in ports.TranscriptSegment) error {
		return st.AddSegment(c, receipt.Segment{ID: in.ID, SessionID: in.SessionID, Start: in.Start, End: in.End, Text: in.Text, SpeakerID: in.SpeakerID, Attribution: string(in.Attribution)})
	})
	chat := commands.ChatSink{Service: &capService, Authority: commands.Authority{ChannelID: channel.ID, SessionID: session.ID}, OnResult: func(r commands.Result) { captured = r.Receipt }}
	if e = capF.Run(ctx, replay.Sinks{Transcript: transcript, Chat: chat}, clock); e != nil {
		return Result{}, e
	}
	if captured == nil {
		return Result{}, errors.New("recorded capture did not create a receipt")
	}
	loop, e := openFixture("../../testdata/replay/t24-loop.json")
	if e != nil {
		return Result{}, e
	}
	if e = runOne(ctx, loop, 0, replay.Sinks{Clip: clipSink(func(c context.Context, in ports.ClipResult) error {
		return st.CompleteClipJob(c, channel.ID, in.RequestID, string(in.Status), "", "", "offline")
	})}, clock); e != nil {
		return Result{}, e
	}
	judgeBytes, e := recorded.ReadFile("fixtures/t22-judge.json")
	if e != nil {
		return Result{}, e
	}
	judge, e := detect.LoadRecorded(bytes.NewReader(judgeBytes))
	if e != nil {
		return Result{}, e
	}
	d := detect.Service{Store: st, Runtime: manager, Judge: judge, Sensitive: safe{}, Now: clock.Now}
	events := []string{"receipt captured from recorded transcript"}
	if e = runOne(ctx, loop, 1, replay.Sinks{Transcript: transcriptSink(func(c context.Context, in ports.TranscriptSegment) error {
		seg := receipt.Segment{ID: in.ID, SessionID: in.SessionID, Text: in.Text, Attribution: string(in.Attribution), SpeakerID: in.SpeakerID, Start: in.Start, End: in.End}
		if e := st.AddSegment(c, seg); e != nil {
			return e
		}
		if e := d.Check(c, a, captured.ID, seg); e != nil {
			return e
		}
		events = append(events, "contradiction detected by recorded judge")
		return nil
	})}, clock); e != nil {
		return Result{}, e
	}
	ap := appeal.Service{Store: st, Runtime: manager, Duration: time.Minute, Now: clock.Now}
	if e = ap.Answer(ctx, a, captured.ID, "demo-answer", "appeal", ""); e != nil {
		return Result{}, e
	}
	events = append(events, "appeal opened")
	if e = runOne(ctx, loop, 2, replay.Sinks{Chat: appeal.ChatSink{Service: ap, Authority: a}}, clock); e != nil {
		return Result{}, e
	}
	events = append(events, "recorded chat vote applied")
	var text string
	if e = runOne(ctx, loop, 3, replay.Sinks{Chat: chatSink(func(c context.Context, cmd ports.ChatCommand) error {
		var err error
		text, err = (lore.Service{Ledger: st}).Handle(c, channel.ID, cmd)
		return err
	})}, clock); e != nil {
		return Result{}, e
	}
	if e = ap.Recover(ctx, a); e != nil {
		return Result{}, e
	}
	text, e = (lore.Service{Ledger: st}).Render(ctx, channel.ID)
	if e != nil {
		return Result{}, e
	}
	rows, e := st.PublicReceipts(ctx, channel.ID, 50)
	if e != nil {
		return Result{}, e
	}
	verdict := ""
	if len(rows) > 0 {
		verdict = string(rows[0].Verdict)
	}
	clip, e := st.ClipJobForReceipt(ctx, channel.ID, captured.ID)
	if e != nil {
		return Result{}, e
	}
	return Result{Stage: "complete", Claim: captured.Claim, Verdict: verdict, ClipStatus: string(clip.Status), Lore: text, Events: events}, nil
}
func openFixture(path string) (replay.Fixture, error) {
	name := filepath.Base(path)
	data, e := recorded.ReadFile("fixtures/" + name)
	if e != nil {
		return replay.Fixture{}, e
	}
	return replay.Decode(bytes.NewReader(data))
}
func runOne(ctx context.Context, f replay.Fixture, i int, s replay.Sinks, c *replay.Clock) error {
	one := f
	one.Inputs = []replay.Input{f.Inputs[i]}
	return one.Run(ctx, s, c)
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

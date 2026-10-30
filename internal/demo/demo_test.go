package demo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type forbiddenTransport struct{ calls atomic.Int32 }

func (f *forbiddenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls.Add(1)
	return nil, context.DeadlineExceeded
}

func post(t *testing.T, h http.Handler, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, nil)
	r.Header.Set("Origin", "http://"+r.Host)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestVisitorIsolationReplayResetAndNoProviderConfigurationDependency(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-only-key-must-not-be-used")
	t.Setenv("TWITCH_CLIENT_SECRET", "test-only-secret-must-not-be-used")
	t.Setenv("TWITCH_ACCESS_TOKEN", "test-only-token-must-not-be-used")
	transport := &forbiddenTransport{}
	oldTransport := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = oldTransport }()
	h := New(t.TempDir(), 4)
	defer h.Close()
	v := &visitor{}
	if err := h.recreate(v); err != nil {
		t.Fatal(err)
	}
	if _, err := h.runReplay(t.Context(), v); err != nil {
		t.Fatalf("run replay: %v", err)
	}
	_ = h.dispose(v)
	a := post(t, h, nil, "/demo/api/replay")
	if a.Code != 200 {
		t.Fatalf("replay A status %d: %s", a.Code, a.Body.String())
	}
	ca := a.Result().Cookies()[0]
	if !ca.HttpOnly || !ca.Secure || ca.SameSite != http.SameSiteStrictMode || ca.Value == "" || ca.Path != "/demo" {
		t.Fatalf("unsafe visitor cookie: %+v", ca)
	}
	if !strings.Contains(a.Body.String(), `"verdict":"upheld"`) || !strings.Contains(a.Body.String(), `"clip_status":"failed"`) || !strings.Contains(a.Body.String(), "Lore: 1 total | 0 open, 1 broken") {
		t.Fatalf("real service replay result: %s", a.Body.String())
	}
	if transport.calls.Load() != 0 {
		t.Fatalf("demo made %d provider HTTP calls", transport.calls.Load())
	}
	b := post(t, h, nil, "/demo/api/replay")
	if b.Code != 200 {
		t.Fatalf("replay B status %d: %s", b.Code, b.Body.String())
	}
	cb := b.Result().Cookies()[0]
	if ca.Value == cb.Value {
		t.Fatal("visitors received the same token")
	}
	if e := post(t, h, ca, "/demo/api/reset"); e.Code != 200 {
		t.Fatalf("reset status %d", e.Code)
	}
	b2 := post(t, h, cb, "/demo/api/replay")
	if !strings.Contains(b2.Body.String(), `"verdict":"upheld"`) {
		t.Fatalf("visitor A reset changed visitor B: %s", b2.Body.String())
	}
	a2 := post(t, h, ca, "/demo/api/replay")
	if !strings.Contains(a2.Body.String(), `"verdict":"upheld"`) {
		t.Fatalf("visitor A replay after reset: %s", a2.Body.String())
	}
	// Client supplied channel/session selectors are ignored and cannot select another ledger.
	guess := post(t, h, ca, "/demo/api/replay?channel_id=00000000000000000000000000000001&session_id=00000000000000000000000000000002")
	if guess.Code != 200 {
		t.Fatalf("guessed selector changed replay: %d", guess.Code)
	}
	if post(t, h, ca, "/demo/api/reset").Code != 200 {
		t.Fatal("reset failed")
	}
	if post(t, h, ca, "/demo/api/reset").Code != 200 {
		t.Fatal("second reset failed")
	}
	if len(h.visitors) != 2 {
		t.Fatalf("visitor count %d", len(h.visitors))
	}
}

func TestVisitorCapacityExpiryAndConcurrentIsolation(t *testing.T) {
	root := t.TempDir()
	h := New(root, 2)
	defer h.Close()
	one := post(t, h, nil, "/demo/api/replay")
	two := post(t, h, nil, "/demo/api/replay")
	if one.Code != 200 || two.Code != 200 {
		t.Fatalf("visitor replays status %d/%d", one.Code, two.Code)
	}
	full := post(t, h, nil, "/demo/api/replay")
	if full.Code != 503 {
		t.Fatalf("capacity status %d", full.Code)
	}
	var oldDir string
	for _, v := range h.visitors {
		oldDir = v.path
		break
	}
	if _, e := os.Stat(oldDir); e != nil {
		t.Fatal(e)
	}
	h.mu.Lock()
	expiredAt := time.Now().Add(-visitorTTL - time.Second).UnixNano()
	for _, v := range h.visitors {
		v.created.Store(expiredAt)
	}
	h.mu.Unlock()
	w := post(t, h, nil, "/demo/api/replay")
	if w.Code != 200 {
		t.Fatalf("expired capacity not reclaimed: %d %s", w.Code, w.Body.String())
	}
	if _, e := os.Stat(oldDir); !os.IsNotExist(e) {
		t.Fatalf("expired visitor files remain at %s", filepath.Clean(oldDir))
	}
}

func TestConcurrentReplayAndResetForOneVisitor(t *testing.T) {
	h := New(t.TempDir(), 2)
	defer h.Close()
	first := post(t, h, nil, "/demo/api/reset")
	if first.Code != 200 {
		t.Fatalf("initial reset status %d", first.Code)
	}
	cookie := first.Result().Cookies()[0]
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			action := "/demo/api/replay"
			if i%2 == 0 {
				action = "/demo/api/reset"
			}
			w := post(t, h, cookie, action)
			if w.Code != 200 && w.Code != http.StatusTooManyRequests {
				t.Errorf("concurrent %s status %d: %s", action, w.Code, w.Body.String())
			}
		}(i)
	}
	wg.Wait()
	last := post(t, h, cookie, "/demo/api/replay")
	if last.Code != 200 || !strings.Contains(last.Body.String(), `"verdict":"upheld"`) {
		t.Fatalf("final replay status %d: %s", last.Code, last.Body.String())
	}
}

func TestExpirySkipsLeasedVisitorAndBusyRequestsDoNotWait(t *testing.T) {
	h := New(t.TempDir(), 2)
	defer h.Close()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/demo/api/reset", nil)
	r.Header.Set("Origin", "http://"+r.Host)
	v, err := h.acquireVisitor(w, r)
	if err != nil {
		t.Fatal(err)
	}
	path := v.path
	v.created.Store(time.Now().Add(-visitorTTL - time.Second).UnixNano())
	h.expire()
	if _, err = os.Stat(path); err != nil {
		t.Fatalf("janitor removed leased visitor files: %v", err)
	}
	if v.ledger == nil {
		t.Fatal("janitor closed a leased ledger")
	}
	h.release(v)
	h.expire()
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("janitor kept expired visitor after release: %v", err)
	}

	ready := post(t, h, nil, "/demo/api/reset")
	if ready.Code != 200 {
		t.Fatalf("new visitor reset: %d", ready.Code)
	}
	cookie := ready.Result().Cookies()[0]
	h.mu.Lock()
	var current *visitor
	for _, candidate := range h.visitors {
		current = candidate
		break
	}
	h.mu.Unlock()
	current.mu.Lock()
	start := time.Now()
	busy := post(t, h, cookie, "/demo/api/replay")
	elapsed := time.Since(start)
	current.mu.Unlock()
	if busy.Code != http.StatusTooManyRequests || elapsed > time.Second {
		t.Fatalf("contended visitor response status=%d elapsed=%s", busy.Code, elapsed)
	}
	h.mu.Lock()
	refs := current.refs.Load()
	h.mu.Unlock()
	if refs != 0 {
		t.Fatalf("busy request leaked %d visitor refs", refs)
	}
	if retry := post(t, h, cookie, "/demo/api/replay"); retry.Code != 200 {
		t.Fatalf("visitor did not recover after contention: %d", retry.Code)
	}

	closeResponse := httptest.NewRecorder()
	closeRequest := httptest.NewRequest(http.MethodPost, "/demo/api/reset", nil)
	closeRequest.Header.Set("Origin", "http://"+closeRequest.Host)
	leased, err := h.acquireVisitor(closeResponse, closeRequest)
	if err != nil {
		t.Fatal(err)
	}
	closePath := leased.path
	closed := make(chan error, 1)
	go func() { closed <- h.Close() }()
	deadline := time.Now().Add(time.Second)
	for {
		h.mu.Lock()
		closing := h.closed
		h.mu.Unlock()
		if closing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Close did not begin")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned with visitor request in flight: %v", err)
	default:
	}
	h.release(leased)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(closePath); !os.IsNotExist(err) {
		t.Fatalf("Close kept leased visitor files: %v", err)
	}
}

func TestCanceledReplayResetsPartialStateAndCloseRejectsNewVisitors(t *testing.T) {
	h := New(t.TempDir(), 2)
	defer h.Close()
	first := post(t, h, nil, "/demo/api/reset")
	cookie := first.Result().Cookies()[0]
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest(http.MethodPost, "/demo/api/replay", nil).WithContext(ctx)
	r.Header.Set("Origin", "http://"+r.Host)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("canceled replay status %d", w.Code)
	}
	retried := post(t, h, cookie, "/demo/api/replay")
	if retried.Code != http.StatusOK || !strings.Contains(retried.Body.String(), `"verdict":"upheld"`) {
		t.Fatalf("replay retry status %d: %s", retried.Code, retried.Body.String())
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	after := post(t, h, cookie, "/demo/api/replay")
	if after.Code != http.StatusServiceUnavailable {
		t.Fatalf("request after close status %d", after.Code)
	}
	if len(h.visitors) != 0 {
		t.Fatalf("close retained %d visitor containers", len(h.visitors))
	}
}

func TestRejectsUnboundedBodyAndUnknownRoutes(t *testing.T) {
	h := New(t.TempDir(), 1)
	defer h.Close()
	r := httptest.NewRequest(http.MethodPost, "/demo/api/reset", strings.NewReader(strings.Repeat("x", 1<<20)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Fatalf("body request status %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/demo/api/private", nil))
	if w.Code != 404 {
		t.Fatalf("unknown route status %d", w.Code)
	}
	if len(h.visitors) != 0 {
		t.Fatalf("invalid requests allocated %d visitors", len(h.visitors))
	}
	r = httptest.NewRequest(http.MethodPost, "http://demo.example/demo/api/reset", nil)
	r.Header.Set("Origin", "https://attacker.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Fatalf("cross-origin status %d", w.Code)
	}
	if len(h.visitors) != 0 {
		t.Fatal("cross-origin request allocated a visitor")
	}
}

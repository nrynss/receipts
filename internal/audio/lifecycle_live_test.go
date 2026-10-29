//go:build live

package audio

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"receipts/internal/twitch"
)

// TestLiveCaptureLifecycle has no model spend or chat/clip writes. Its budget is
// eight minutes, at most 240 Helix checks, and two capture attempts. All audio is
// temporary and deleted. The second attempt stays active until the owner stops
// the actual broadcast, rather than ending on a short audio-duration timer.
func TestLiveCaptureLifecycle(t *testing.T) {
	if os.Getenv("RECEIPTS_LIVE_PROBE") != "1" {
		t.Skip("explicit live probe enablement required")
	}
	need := func(name string) string {
		t.Helper()
		v := os.Getenv(name)
		if v == "" {
			t.Fatalf("missing %s", name)
		}
		return v
	}
	client := twitch.Client{ClientID: need("TWITCH_CLIENT_ID"), Token: need("TWITCH_ACCESS_TOKEN")}
	broadcaster := need("TWITCH_BROADCASTER_ID")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	identity, err := client.Validate(ctx)
	if err != nil {
		t.Fatal("Twitch authorization validation failed; no capture started")
	}
	if identity.ClientID != client.ClientID || identity.UserID != broadcaster || !strings.EqualFold(identity.Login, "nrynss") {
		t.Fatal("only the authorized nrynss broadcaster can run this probe")
	}
	streamlink := os.Getenv("RECEIPTS_STREAMLINK_BIN")
	if streamlink == "" {
		streamlink = "streamlink"
	}
	var checks atomic.Int32
	var offlineAt atomic.Int64
	check := func(ctx context.Context) (bool, error) {
		if checks.Add(1) > 240 {
			return false, errors.New("live-check budget exhausted")
		}
		live, err := client.StreamLive(ctx, broadcaster)
		if err == nil && !live {
			offlineAt.Store(time.Now().UnixNano())
		}
		return live, err
	}
	t.Log("WATCHER_READY: start the nrynss stream with original content. Do not stop it until RECONNECTED_CAPTURE_ACTIVE.")
	waitCtx, stopWaiting := context.WithTimeout(ctx, 3*time.Minute)
	defer stopWaiting()
	consecutiveLive := 0
	for {
		live, err := check(waitCtx)
		if err != nil {
			t.Fatal("initial live-status check failed")
		}
		if live {
			consecutiveLive++
		} else {
			consecutiveLive = 0
		}
		if consecutiveLive >= 2 {
			break
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("stream did not start within probe window")
		case <-time.After(2 * time.Second):
		}
	}
	type attempt struct {
		dir     string
		cancel  context.CancelFunc
		done    chan struct{}
		err     error
		endedAt time.Time
	}
	start := func(name string) *attempt {
		aCtx, aCancel := context.WithCancel(ctx)
		a := &attempt{dir: filepath.Join(t.TempDir(), name), cancel: aCancel, done: make(chan struct{})}
		go func() {
			a.err = CaptureWhileLive(aCtx, check, func(c context.Context) error {
				return CaptureUntilCanceled(c, streamlink, "https://www.twitch.tv/nrynss", a.dir)
			}, 2*time.Second)
			a.endedAt = time.Now()
			close(a.done)
		}()
		t.Cleanup(func() {
			a.cancel()
			select {
			case <-a.done:
			case <-time.After(5 * time.Second):
				t.Error("capture subprocess cleanup did not finish")
			}
		})
		return a
	}
	waitForAudio := func(a *attempt) {
		t.Helper()
		deadline := time.NewTimer(45 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-a.done:
				t.Fatalf("capture ended before audio was ready: %v", a.err)
			case <-deadline.C:
				t.Fatal("no audio data arrived within readiness window")
			case <-ticker.C:
				paths, _ := filepath.Glob(filepath.Join(a.dir, "chunk-*.wav"))
				for _, p := range paths {
					info, err := os.Stat(p)
					if err == nil && info.Size() > 4096 {
						return
					}
				}
			}
		}
	}
	first := start("first")
	waitForAudio(first)
	t.Log("CAPTURE_ACTIVE: audio bytes observed. Exercising one controlled pipeline interruption.")
	interrupted := time.Now()
	first.cancel()
	select {
	case <-first.done:
	case <-time.After(3 * time.Second):
		t.Fatal("interrupted pipeline did not stop within three seconds")
	}
	if !errors.Is(first.err, context.Canceled) {
		t.Fatalf("unexpected interruption result: %v", first.err)
	}
	interruptLatency := first.endedAt.Sub(interrupted)
	if err := os.RemoveAll(first.dir); err != nil {
		t.Fatal("could not discard interrupted audio")
	}
	second := start("reconnected")
	waitForAudio(second)
	reconnectedAt := time.Now().UTC()
	t.Log("RECONNECTED_CAPTURE_ACTIVE: audio resumed after a fresh source connection. Stop broadcasting now; this capture stays active until Twitch reports offline or the source ends.")
	select {
	case <-second.done:
	case <-ctx.Done():
		t.Fatal("broadcast did not end within the eight-minute probe budget")
	}
	if !errors.Is(second.err, ErrOffline) && !errors.Is(second.err, ErrSourceEnded) {
		t.Fatalf("capture ended without confirmed offline/source EOF: %v", second.err)
	}
	// The stream source may close before Helix reflects the end. Confirm the
	// authoritative offline result separately without restarting capture.
	if !errors.Is(second.err, ErrOffline) {
		confirmCtx, stop := context.WithTimeout(ctx, 25*time.Second)
		defer stop()
		for {
			live, err := check(confirmCtx)
			if err != nil {
				t.Fatal("could not confirm offline after source EOF")
			}
			if !live {
				break
			}
			select {
			case <-confirmCtx.Done():
				t.Fatal("source ended but Twitch still reports live")
			case <-time.After(2 * time.Second):
			}
		}
	}
	observedOffline := time.Unix(0, offlineAt.Load()).UTC()
	if observedOffline.Before(reconnectedAt) {
		t.Fatal("no post-reconnect offline observation")
	}
	shutdownLatency := second.endedAt.Sub(observedOffline)
	if shutdownLatency < 0 {
		shutdownLatency = 0
	}
	if errors.Is(second.err, ErrOffline) && shutdownLatency > 3*time.Second {
		t.Fatal("offline-triggered cleanup exceeded three seconds")
	}
	if err := os.RemoveAll(second.dir); err != nil {
		t.Fatal("could not discard final audio")
	}
	evidence := struct {
		Channel                string    `json:"channel"`
		ControlledInterruption bool      `json:"controlled_interruption"`
		InterruptionCleanupMS  int64     `json:"interruption_cleanup_ms"`
		ReconnectAudioObserved bool      `json:"reconnect_audio_observed"`
		ReconnectedAt          time.Time `json:"reconnected_at"`
		OfflineObservedAt      time.Time `json:"offline_observed_at"`
		CaptureEndedAt         time.Time `json:"capture_ended_at"`
		OfflineCleanupMS       int64     `json:"offline_cleanup_ms"`
		SourceEndedFirst       bool      `json:"source_ended_before_offline_confirmation"`
		StatusChecks           int32     `json:"status_checks"`
		AudioRetained          bool      `json:"audio_retained"`
		ModelSpend             int       `json:"model_spend"`
	}{"nrynss", true, interruptLatency.Milliseconds(), true, reconnectedAt, observedOffline, second.endedAt.UTC(), shutdownLatency.Milliseconds(), errors.Is(second.err, ErrSourceEnded), checks.Load(), false, 0}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal("could not encode lifecycle evidence")
	}
	if output := os.Getenv("RECEIPTS_LIFECYCLE_PROBE_OUTPUT"); output != "" {
		if os.WriteFile(output, append(data, '\n'), 0o600) != nil {
			t.Fatal("could not save lifecycle evidence")
		}
	}
	t.Logf("Lifecycle passed: interruption cleanup %s, reconnect audio observed, actual broadcast end confirmed; offline cleanup %s; all audio discarded.", interruptLatency, shutdownLatency)
}

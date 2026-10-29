//go:build live

package audio

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"receipts/internal/twitch"
)

// TestLiveStreamlinkCapture verifies live-status checks, timestamped audio
// chunks, and subprocess release using the selected nrynss broadcast.
func TestLiveStreamlinkCapture(t *testing.T) {
	if os.Getenv("RECEIPTS_LIVE_PROBE") != "1" {
		t.Skip("set RECEIPTS_LIVE_PROBE=1 to enable the bounded own-channel audio probe")
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
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	identity, err := client.Validate(ctx)
	if err != nil {
		t.Fatal("Twitch token validation failed")
	}
	if identity.ClientID != client.ClientID || identity.UserID != broadcaster || !strings.EqualFold(identity.Login, "nrynss") {
		t.Fatal("audio probe requires the designated nrynss broadcaster's own user token and matching app")
	}
	live, err := client.StreamLive(ctx, broadcaster)
	if err != nil {
		t.Fatal("Twitch live-status check failed")
	}
	if !live {
		t.Fatal("nrynss must be live before audio capture")
	}
	seconds := 10
	if raw := os.Getenv("RECEIPTS_PROBE_SECONDS"); raw != "" {
		seconds, err = strconv.Atoi(raw)
		if err != nil || seconds < 5 || seconds > 30 {
			t.Fatal("RECEIPTS_PROBE_SECONDS must be between 5 and 30")
		}
	}
	streamlink := os.Getenv("RECEIPTS_STREAMLINK_BIN")
	if streamlink == "" {
		streamlink = "streamlink"
	}
	outputDir, err := os.MkdirTemp("", "receipts-audio-probe-")
	if err != nil {
		t.Fatal("could not create temporary audio directory")
	}
	defer os.RemoveAll(outputDir)
	chunks, err := CaptureStreamlink(ctx, streamlink, "https://www.twitch.tv/nrynss", outputDir, time.Duration(seconds)*time.Second)
	if err != nil {
		t.Fatalf("bounded audio capture failed: %v", err)
	}
	stillLive, err := client.StreamLive(ctx, broadcaster)
	if err != nil {
		t.Fatal("final Twitch live-status check failed")
	}
	if !stillLive {
		t.Fatal("channel ended during capture; discard partial audio and rerun during a live session")
	}
	evidence := struct {
		Channel          string `json:"channel"`
		Source           string `json:"source"`
		RequestedSeconds int    `json:"requested_seconds"`
		Chunks           []struct {
			Sequence   int           `json:"sequence"`
			CapturedAt time.Time     `json:"captured_at_estimate"`
			Duration   time.Duration `json:"duration"`
			Bytes      int64         `json:"bytes"`
		} `json:"chunks"`
		LiveBefore    bool `json:"live_before"`
		LiveAfter     bool `json:"live_after"`
		AudioRetained bool `json:"audio_retained"`
	}{Channel: "nrynss", Source: "Streamlink to Keel-managed ffmpeg", RequestedSeconds: seconds, LiveBefore: true, LiveAfter: true, AudioRetained: false}
	for _, chunk := range chunks {
		info, err := os.Stat(chunk.Path)
		if err != nil {
			t.Fatal("audio chunk disappeared before evidence collection")
		}
		evidence.Chunks = append(evidence.Chunks, struct {
			Sequence   int           `json:"sequence"`
			CapturedAt time.Time     `json:"captured_at_estimate"`
			Duration   time.Duration `json:"duration"`
			Bytes      int64         `json:"bytes"`
		}{chunk.Sequence, chunk.CapturedAt, chunk.Duration, info.Size()})
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal("could not encode sanitized audio evidence")
	}
	if path := os.Getenv("RECEIPTS_AUDIO_PROBE_OUTPUT"); path != "" {
		if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
			t.Fatal("could not write sanitized audio evidence")
		}
	}
	t.Logf("Captured %d timestamp-estimated chunks for %ds; audio files were deleted after the probe.", len(chunks), seconds)
}

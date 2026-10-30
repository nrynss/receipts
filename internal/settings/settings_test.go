package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func env(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}

func TestLoadFixtureWithoutSecrets(t *testing.T) {
	s, err := Load(context.Background(), env(map[string]string{
		"RECEIPTS_MODE":                 "fixture",
		"RECEIPTS_TRANSCRIPT_RETENTION": "45m",
		"RECEIPTS_APPEAL_DURATION":      "90s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode != ModeFixture || s.PublicOrigin != "http://localhost:8080" {
		t.Fatalf("loaded settings = %#v", s)
	}
	if s.TranscriptRetention != 45*time.Minute || s.AppealDuration != 90*time.Second {
		t.Fatalf("duration overrides = retention %s appeal %s", s.TranscriptRetention, s.AppealDuration)
	}
}

func TestLoadLiveReportsMissingNamesWithoutSecretValues(t *testing.T) {
	const sentinel = "secret-sentinel"
	_, err := Load(context.Background(), env(map[string]string{
		"RECEIPTS_MODE": "live", "RECEIPTS_CHANNEL_ID": "channel-id",
		"RECEIPTS_CHANNEL_NAME": "owner", "TWITCH_CLIENT_ID": sentinel,
		"TWITCH_ACCESS_TOKEN": sentinel,
	}))
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("error = %v, want missing OpenAI key name", err)
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("error leaked secret value: %v", err)
	}
}

func TestLoadLiveWithConfiguredSettings(t *testing.T) {
	s, err := Load(context.Background(), env(map[string]string{
		"RECEIPTS_MODE": "live", "RECEIPTS_CHANNEL_ID": "channel-id",
		"RECEIPTS_CHANNEL_NAME": "owner", "RECEIPTS_TRANSCRIPTION_MODEL": "gpt-transcribe",
		"RECEIPTS_EMBEDDING_MODEL": "model-embedding", "RECEIPTS_JUDGE_MODEL": "model-judge",
		"TWITCH_CLIENT_ID": "client-secret-sentinel", "TWITCH_ACCESS_TOKEN": "token-secret-sentinel",
		"OPENAI_API_KEY": "openai-secret-sentinel",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode != ModeLive || s.JudgeModel != "model-judge" {
		t.Fatalf("live settings = %#v", s)
	}
	formatted := fmt.Sprintf("%+v %#v", s, s)
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, sentinel := range []string{"client-secret-sentinel", "token-secret-sentinel", "openai-secret-sentinel"} {
		if strings.Contains(formatted, sentinel) || strings.Contains(string(encoded), sentinel) {
			t.Fatalf("settings formatting leaked secret sentinel %q", sentinel)
		}
	}
}

func TestLoadHostedAuthUsesOperatorKeysWithoutSharedChannelToken(t *testing.T) {
	const encryptionKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	s, err := Load(context.Background(), env(map[string]string{
		"RECEIPTS_MODE": "live", "RECEIPTS_AUTH_ENABLED": "true",
		"RECEIPTS_PUBLIC_ORIGIN":       "https://receipts.example",
		"RECEIPTS_TRANSCRIPTION_MODEL": "gpt-transcribe", "RECEIPTS_EMBEDDING_MODEL": "model-embedding", "RECEIPTS_JUDGE_MODEL": "model-judge",
		"TWITCH_CLIENT_ID": "app-id", "TWITCH_CLIENT_SECRET": "client-secret-value",
		"RECEIPTS_TOKEN_ENCRYPTION_KEY": encryptionKey, "OPENAI_API_KEY": "openai-key",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !s.AuthEnabled || s.ChannelID != "" {
		t.Fatalf("hosted settings = %#v", s)
	}
	if _, err := s.TwitchAccessToken.Reveal(); err == nil {
		t.Fatal("hosted auth unexpectedly loaded a shared Twitch access token")
	}
	formatted := fmt.Sprintf("%+v %#v", s, s)
	for _, value := range []string{"client-secret-value", encryptionKey, "openai-key"} {
		if strings.Contains(formatted, value) {
			t.Fatalf("hosted settings leaked %q", value)
		}
	}
}

func TestHostedAuthRequiresEncryptionKeyAndHTTPS(t *testing.T) {
	base := map[string]string{
		"RECEIPTS_MODE": "live", "RECEIPTS_AUTH_ENABLED": "true",
		"RECEIPTS_PUBLIC_ORIGIN":       "http://receipts.example",
		"RECEIPTS_TRANSCRIPTION_MODEL": "gpt-transcribe", "RECEIPTS_EMBEDDING_MODEL": "model-embedding", "RECEIPTS_JUDGE_MODEL": "model-judge",
		"TWITCH_CLIENT_ID": "app-id", "TWITCH_CLIENT_SECRET": "client-secret-value", "OPENAI_API_KEY": "openai-key",
	}
	if _, err := Load(context.Background(), env(base)); err == nil || !strings.Contains(err.Error(), "RECEIPTS_TOKEN_ENCRYPTION_KEY") {
		t.Fatalf("missing key error = %v", err)
	}
	base["RECEIPTS_TOKEN_ENCRYPTION_KEY"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	if _, err := Load(context.Background(), env(base)); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("HTTP origin error = %v", err)
	}
}

func TestValidateRejectsInvalidOriginAndLimits(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Settings)
	}{
		{"origin credentials", func(s *Settings) { s.PublicOrigin = "https://user:pass@example.test" }},
		{"origin path", func(s *Settings) { s.PublicOrigin = "https://example.test/path" }},
		{"origin empty hostname", func(s *Settings) { s.PublicOrigin = "http://:8080" }},
		{"origin port zero", func(s *Settings) { s.PublicOrigin = "https://example.test:0" }},
		{"origin port too large", func(s *Settings) { s.PublicOrigin = "https://example.test:99999" }},
		{"zero retention", func(s *Settings) { s.TranscriptRetention = 0 }},
		{"negative cooldown", func(s *Settings) { s.StampCooldown = -time.Second }},
		{"long appeal", func(s *Settings) { s.AppealDuration = 25 * time.Hour }},
		{"stale status", func(s *Settings) { s.LiveStatusFreshness = 0 }},
		{"zero spend", func(s *Settings) { s.JudgeSpendUSD = 0 }},
		{"nan spend", func(s *Settings) { s.JudgeSpendUSD = math.NaN() }},
		{"zero global channel capacity", func(s *Settings) { s.MaxActiveChannels = 0 }},
		{"channel spend exceeds global", func(s *Settings) { s.ChannelSpendLimitUSD = s.GlobalSpendLimitUSD + 1 }},
		{"infinite global spend", func(s *Settings) { s.GlobalSpendLimitUSD = math.Inf(1) }},
		{"invalid listen address", func(s *Settings) { s.HTTPAddress = "bad" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := Defaults()
			s.Mode = ModeFixture
			tc.edit(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("Validate succeeded")
			}
		})
	}
}

func TestLoadRejectsInfiniteSpendLimits(t *testing.T) {
	for _, spend := range []string{"+Inf", "-Inf"} {
		t.Run(spend, func(t *testing.T) {
			_, err := Load(context.Background(), env(map[string]string{
				"RECEIPTS_MODE": "fixture", "RECEIPTS_JUDGE_SPEND_USD": spend,
			}))
			if err == nil || !strings.Contains(err.Error(), "RECEIPTS_JUDGE_SPEND_USD") {
				t.Fatalf("Load error = %v, want invalid spend cap", err)
			}
		})
	}
}

func TestModeMustBeExplicit(t *testing.T) {
	if _, err := Load(context.Background(), env(nil)); err == nil || !strings.Contains(err.Error(), "RECEIPTS_MODE") {
		t.Fatalf("error = %v, want explicit mode error", err)
	}
}

func TestSessionSpendLimitOverride(t *testing.T) {
	s, err := Load(context.Background(), env(map[string]string{"RECEIPTS_MODE": "fixture", "RECEIPTS_SESSION_SPEND_LIMIT_USD": "0.25"}))
	if err != nil {
		t.Fatal(err)
	}
	if s.SessionSpendLimitUSD != 0.25 {
		t.Fatalf("session limit=%v", s.SessionSpendLimitUSD)
	}
}

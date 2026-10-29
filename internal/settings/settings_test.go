package settings

import (
	"context"
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
		"RECEIPTS_CHANNEL_NAME": "owner", "RECEIPTS_TRANSCRIPTION_MODEL": "model-stt",
		"RECEIPTS_EMBEDDING_MODEL": "model-embedding", "RECEIPTS_JUDGE_MODEL": "model-judge",
		"TWITCH_CLIENT_ID": "client-id", "TWITCH_ACCESS_TOKEN": "token-value",
		"OPENAI_API_KEY": "api-key-value",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode != ModeLive || s.JudgeModel != "model-judge" {
		t.Fatalf("live settings = %#v", s)
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
		{"zero retention", func(s *Settings) { s.TranscriptRetention = 0 }},
		{"negative cooldown", func(s *Settings) { s.StampCooldown = -time.Second }},
		{"long appeal", func(s *Settings) { s.AppealDuration = 25 * time.Hour }},
		{"stale status", func(s *Settings) { s.LiveStatusFreshness = 0 }},
		{"zero spend", func(s *Settings) { s.JudgeSpendUSD = 0 }},
		{"nan spend", func(s *Settings) { s.JudgeSpendUSD = math.NaN() }},
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

func TestModeMustBeExplicit(t *testing.T) {
	if _, err := Load(context.Background(), env(nil)); err == nil || !strings.Contains(err.Error(), "RECEIPTS_MODE") {
		t.Fatalf("error = %v, want explicit mode error", err)
	}
}

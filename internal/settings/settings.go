// Package settings owns process configuration and validates the boundary
// between deterministic fixture runs and live integrations.
package settings

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nrynss/keel/config"
	"github.com/nrynss/keel/config/source"
)

type Mode string

const (
	ModeFixture Mode = "fixture"
	ModeLive    Mode = "live"
)

// Settings contains only process-wide behavior. Secret values remain inside
// Keel Secret and are never included in settings formatting.
type Settings struct {
	Mode                  Mode          `toml:"mode"`
	PublicOrigin          string        `toml:"public_origin"`
	ChannelID             string        `toml:"channel_id"`
	ChannelName           string        `toml:"channel_name"`
	TranscriptRetention   time.Duration `toml:"transcript_retention"`
	StampCooldown         time.Duration `toml:"stamp_cooldown"`
	AppealDuration        time.Duration `toml:"appeal_duration"`
	LiveStatusFreshness   time.Duration `toml:"live_status_freshness"`
	TranscriptionModel    string        `toml:"transcription_model"`
	EmbeddingModel        string        `toml:"embedding_model"`
	JudgeModel            string        `toml:"judge_model"`
	TranscriptionSpendUSD float64       `toml:"transcription_spend_usd"`
	EmbeddingSpendUSD     float64       `toml:"embedding_spend_usd"`
	JudgeSpendUSD         float64       `toml:"judge_spend_usd"`
	HTTPAddress           string        `toml:"http_address"`
	DatabasePath          string        `toml:"database_path"`
	AuthEnabled           bool          `toml:"auth_enabled"`
	TwitchClientID        config.Secret `toml:"twitch_client_id"`
	TwitchAccessToken     config.Secret `toml:"twitch_access_token"`
	TwitchClientSecret    config.Secret `toml:"twitch_client_secret"`
	TokenEncryptionKey    config.Secret `toml:"token_encryption_key"`
	OpenAIAPIKey          config.Secret `toml:"openai_api_key"`
}

// configValues uses strings for durations because Keel's scalar environment
// parser intentionally handles numeric values only; the application receives
// validated time.Duration values after parsing Go's standard duration syntax.
type configValues struct {
	Mode                  Mode    `toml:"mode"`
	PublicOrigin          string  `toml:"public_origin"`
	ChannelID             string  `toml:"channel_id"`
	ChannelName           string  `toml:"channel_name"`
	TranscriptRetention   string  `toml:"transcript_retention"`
	StampCooldown         string  `toml:"stamp_cooldown"`
	AppealDuration        string  `toml:"appeal_duration"`
	LiveStatusFreshness   string  `toml:"live_status_freshness"`
	TranscriptionModel    string  `toml:"transcription_model"`
	EmbeddingModel        string  `toml:"embedding_model"`
	JudgeModel            string  `toml:"judge_model"`
	TranscriptionSpendUSD float64 `toml:"transcription_spend_usd"`
	EmbeddingSpendUSD     float64 `toml:"embedding_spend_usd"`
	JudgeSpendUSD         float64 `toml:"judge_spend_usd"`
	HTTPAddress           string  `toml:"http_address"`
	DatabasePath          string  `toml:"database_path"`
	AuthEnabled           bool    `toml:"auth_enabled"`
}

func configDefaults() configValues {
	d := Defaults()
	return configValues{
		Mode: d.Mode, PublicOrigin: d.PublicOrigin, ChannelID: d.ChannelID,
		ChannelName: d.ChannelName, TranscriptRetention: d.TranscriptRetention.String(),
		StampCooldown: d.StampCooldown.String(), AppealDuration: d.AppealDuration.String(),
		LiveStatusFreshness: d.LiveStatusFreshness.String(), TranscriptionModel: d.TranscriptionModel,
		EmbeddingModel: d.EmbeddingModel, JudgeModel: d.JudgeModel,
		TranscriptionSpendUSD: d.TranscriptionSpendUSD, EmbeddingSpendUSD: d.EmbeddingSpendUSD,
		JudgeSpendUSD: d.JudgeSpendUSD, HTTPAddress: d.HTTPAddress, DatabasePath: d.DatabasePath, AuthEnabled: d.AuthEnabled,
	}
}

func (c configValues) settings() (Settings, error) {
	retention, err := time.ParseDuration(c.TranscriptRetention)
	if err != nil {
		return Settings{}, fmt.Errorf("RECEIPTS_TRANSCRIPT_RETENTION: invalid duration")
	}
	cooldown, err := time.ParseDuration(c.StampCooldown)
	if err != nil {
		return Settings{}, fmt.Errorf("RECEIPTS_STAMP_COOLDOWN: invalid duration")
	}
	appeal, err := time.ParseDuration(c.AppealDuration)
	if err != nil {
		return Settings{}, fmt.Errorf("RECEIPTS_APPEAL_DURATION: invalid duration")
	}
	freshness, err := time.ParseDuration(c.LiveStatusFreshness)
	if err != nil {
		return Settings{}, fmt.Errorf("RECEIPTS_LIVE_STATUS_FRESHNESS: invalid duration")
	}
	return Settings{
		Mode: c.Mode, PublicOrigin: c.PublicOrigin, ChannelID: c.ChannelID,
		ChannelName: c.ChannelName, TranscriptRetention: retention, StampCooldown: cooldown,
		AppealDuration: appeal, LiveStatusFreshness: freshness,
		TranscriptionModel: c.TranscriptionModel, EmbeddingModel: c.EmbeddingModel,
		JudgeModel: c.JudgeModel, TranscriptionSpendUSD: c.TranscriptionSpendUSD,
		EmbeddingSpendUSD: c.EmbeddingSpendUSD, JudgeSpendUSD: c.JudgeSpendUSD,
		HTTPAddress: c.HTTPAddress, DatabasePath: c.DatabasePath, AuthEnabled: c.AuthEnabled,
	}, nil
}

var channelNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,25}$`)

func Defaults() Settings {
	return Settings{
		PublicOrigin:          "http://localhost:8080",
		TranscriptRetention:   30 * time.Minute,
		StampCooldown:         30 * time.Second,
		AppealDuration:        2 * time.Minute,
		LiveStatusFreshness:   15 * time.Second,
		TranscriptionSpendUSD: 1,
		EmbeddingSpendUSD:     0.25,
		JudgeSpendUSD:         0.25,
		HTTPAddress:           ":8080",
		DatabasePath:          "data/receipts.db",
	}
}

// Load reads non-secret settings through Keel config. The config loader uses
// RECEIPTS_<FIELD> for overrides; Keel's secret sources use the exact names
// stored in their references (for example OPENAI_API_KEY).
func Load(ctx context.Context, lookup func(string) (string, bool)) (Settings, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	lookupReceipts := func(key string) (string, bool) {
		field := strings.ToLower(key)
		if field == "mode" || field == "public_origin" || field == "channel_id" || field == "channel_name" ||
			field == "transcript_retention" || field == "stamp_cooldown" || field == "appeal_duration" ||
			field == "live_status_freshness" || field == "transcription_model" || field == "embedding_model" ||
			field == "judge_model" || field == "transcription_spend_usd" || field == "embedding_spend_usd" ||
			field == "judge_spend_usd" || field == "http_address" || field == "database_path" || field == "auth_enabled" {
			return lookup("RECEIPTS_" + key)
		}
		return lookup(key)
	}

	reg := config.NewRegistry()
	if err := source.RegisterDefaults(reg, source.Config{LookupEnv: lookup}); err != nil {
		return Settings{}, fmt.Errorf("register config sources: %w", err)
	}
	c := configDefaults()
	_, err := config.Load(ctx, &c, config.Config{LookupEnv: lookupReceipts, Registry: reg})
	if err != nil {
		return Settings{}, fmt.Errorf("load settings: %w", err)
	}
	s, err := c.settings()
	if err != nil {
		return Settings{}, err
	}
	if s.Mode != ModeLive {
		if err := s.Validate(); err != nil {
			return Settings{}, err
		}
		return s, nil
	}
	missing := make([]string, 0, 6)
	requiredSecrets := []string{"TWITCH_CLIENT_ID", "OPENAI_API_KEY"}
	if !s.AuthEnabled {
		requiredSecrets = append(requiredSecrets, "TWITCH_ACCESS_TOKEN")
	}
	for _, key := range requiredSecrets {
		value, ok := lookup(key)
		if !ok || strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	if s.AuthEnabled {
		for _, key := range []string{"TWITCH_CLIENT_SECRET", "RECEIPTS_TOKEN_ENCRYPTION_KEY"} {
			value, ok := lookup(key)
			if !ok || strings.TrimSpace(value) == "" {
				missing = append(missing, key)
			}
		}
	}
	for key, value := range map[string]string{
		"RECEIPTS_TRANSCRIPTION_MODEL": s.TranscriptionModel,
		"RECEIPTS_EMBEDDING_MODEL":     s.EmbeddingModel,
		"RECEIPTS_JUDGE_MODEL":         s.JudgeModel,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) != 0 {
		return Settings{}, fmt.Errorf("live mode missing required settings: %s", strings.Join(missing, ", "))
	}
	// Fixture mode has no secret references at all, so Keel cannot require
	// credentials during a replay. Live mode adds references only (never values).
	type secretValues struct {
		TwitchClientID     config.Secret `toml:"twitch_client_id"`
		TwitchAccessToken  config.Secret `toml:"twitch_access_token"`
		TwitchClientSecret config.Secret `toml:"twitch_client_secret"`
		TokenEncryptionKey config.Secret `toml:"token_encryption_key"`
		OpenAIAPIKey       config.Secret `toml:"openai_api_key"`
	}
	secrets := secretValues{}
	secretRefs := `
[twitch_client_id]
source = "env"
var = "TWITCH_CLIENT_ID"
[openai_api_key]
source = "env"
var = "OPENAI_API_KEY"
`
	if !s.AuthEnabled {
		secretRefs += `
[twitch_access_token]
source = "env"
var = "TWITCH_ACCESS_TOKEN"
`
	}
	if s.AuthEnabled {
		secretRefs += `
[twitch_client_secret]
source = "env"
var = "TWITCH_CLIENT_SECRET"
[token_encryption_key]
source = "env"
var = "RECEIPTS_TOKEN_ENCRYPTION_KEY"
`
	}
	if err := config.Decode([]byte(secretRefs), &secrets, reg); err != nil {
		return Settings{}, fmt.Errorf("initialize secret references: %w", err)
	}
	_, err = config.Load(ctx, &secrets, config.Config{Registry: reg})
	if err != nil {
		return Settings{}, fmt.Errorf("load settings: %w", err)
	}
	s.TwitchClientID = secrets.TwitchClientID
	s.TwitchAccessToken = secrets.TwitchAccessToken
	s.TwitchClientSecret = secrets.TwitchClientSecret
	s.TokenEncryptionKey = secrets.TokenEncryptionKey
	s.OpenAIAPIKey = secrets.OpenAIAPIKey
	if err := s.Validate(); err != nil {
		return Settings{}, err
	}
	return s, nil
}

func (s Settings) Validate() error {
	switch s.Mode {
	case ModeFixture, ModeLive:
	default:
		return errorsf("RECEIPTS_MODE must be explicitly set to fixture or live")
	}
	u, err := url.Parse(s.PublicOrigin)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errorsf("RECEIPTS_PUBLIC_ORIGIN must be an http(s) origin without credentials, path, query, or fragment")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errorsf("RECEIPTS_PUBLIC_ORIGIN port must be between 1 and 65535")
		}
	}
	if s.ChannelID != "" && strings.TrimSpace(s.ChannelID) != s.ChannelID {
		return errorsf("RECEIPTS_CHANNEL_ID must not contain surrounding whitespace")
	}
	if s.Mode == ModeLive {
		if !s.AuthEnabled && (s.ChannelID == "" || !channelNamePattern.MatchString(s.ChannelName)) {
			return errorsf("live mode requires RECEIPTS_CHANNEL_ID and a valid RECEIPTS_CHANNEL_NAME")
		}
		missing := make([]string, 0, 6)
		if !hasSecret(s.TwitchClientID) {
			missing = append(missing, "TWITCH_CLIENT_ID")
		}
		if !s.AuthEnabled && !hasSecret(s.TwitchAccessToken) {
			missing = append(missing, "TWITCH_ACCESS_TOKEN")
		}
		if !hasSecret(s.OpenAIAPIKey) {
			missing = append(missing, "OPENAI_API_KEY")
		}
		if strings.TrimSpace(s.TranscriptionModel) == "" {
			missing = append(missing, "RECEIPTS_TRANSCRIPTION_MODEL")
		}
		if strings.TrimSpace(s.EmbeddingModel) == "" {
			missing = append(missing, "RECEIPTS_EMBEDDING_MODEL")
		}
		if strings.TrimSpace(s.JudgeModel) == "" {
			missing = append(missing, "RECEIPTS_JUDGE_MODEL")
		}
		if len(missing) > 0 {
			return fmt.Errorf("live mode missing required settings: %s", strings.Join(missing, ", "))
		}
	}
	if s.AuthEnabled {
		if s.Mode != ModeLive || !hasSecret(s.TwitchClientID) || !hasSecret(s.TwitchClientSecret) || !hasSecret(s.TokenEncryptionKey) {
			return errorsf("RECEIPTS_AUTH_ENABLED requires live mode and Twitch client ID, Twitch client secret, and token encryption key")
		}
		if u.Scheme != "https" {
			return errorsf("RECEIPTS_AUTH_ENABLED requires an HTTPS public origin")
		}
	}
	if s.TranscriptRetention < time.Minute || s.TranscriptRetention > 24*time.Hour {
		return errorsf("RECEIPTS_TRANSCRIPT_RETENTION must be between 1m and 24h")
	}
	if s.StampCooldown < 0 || s.StampCooldown > 24*time.Hour {
		return errorsf("RECEIPTS_STAMP_COOLDOWN must be between 0 and 24h")
	}
	if s.AppealDuration < time.Second || s.AppealDuration > 24*time.Hour {
		return errorsf("RECEIPTS_APPEAL_DURATION must be between 1s and 24h")
	}
	if s.LiveStatusFreshness < time.Second || s.LiveStatusFreshness > 5*time.Minute {
		return errorsf("RECEIPTS_LIVE_STATUS_FRESHNESS must be between 1s and 5m")
	}
	if s.Mode == ModeLive {
		for key, value := range map[string]string{"TRANSCRIPTION_MODEL": s.TranscriptionModel, "EMBEDDING_MODEL": s.EmbeddingModel, "JUDGE_MODEL": s.JudgeModel} {
			if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n") {
				return fmt.Errorf("RECEIPTS_%s must be a non-empty single-line model ID", key)
			}
		}
	}
	for key, value := range map[string]float64{"TRANSCRIPTION_SPEND_USD": s.TranscriptionSpendUSD, "EMBEDDING_SPEND_USD": s.EmbeddingSpendUSD, "JUDGE_SPEND_USD": s.JudgeSpendUSD} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > 1000 {
			return fmt.Errorf("RECEIPTS_%s must be greater than 0 and at most 1000", key)
		}
	}
	if err := validateHTTPAddress(s.HTTPAddress); err != nil {
		return errorsf("RECEIPTS_HTTP_ADDRESS must be a non-empty single-line address")
	}
	if strings.TrimSpace(s.DatabasePath) == "" || strings.ContainsAny(s.DatabasePath, "\r\n") {
		return errorsf("RECEIPTS_DATABASE_PATH must be a non-empty path")
	}
	return nil
}

func validateHTTPAddress(address string) error {
	if strings.TrimSpace(address) != address || strings.ContainsAny(address, "\r\n") {
		return fmt.Errorf("invalid listen address")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil || strings.ContainsAny(host, " \t") {
		return fmt.Errorf("invalid listen address")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 {
		return fmt.Errorf("invalid listen port")
	}
	return nil
}

func hasSecret(s config.Secret) bool {
	v, err := s.Reveal()
	return err == nil && v != ""
}

func errorsf(message string) error { return fmt.Errorf("settings: %s", message) }

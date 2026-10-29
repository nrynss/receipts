package main

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/auth"
	"receipts/internal/settings"
	"receipts/internal/store"
)

//go:embed all:static
var overlay embed.FS

func main() {
	if err := run(context.Background(), os.LookupEnv, http.ListenAndServe); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, lookup func(string) (string, bool), serve func(string, http.Handler) error) error {
	cfg, err := settings.Load(ctx, lookup)
	if err != nil {
		return err
	}
	db, err := sqlite.Open(ctx, sqlite.Config{Path: cfg.DatabasePath})
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := store.New(ctx, db, store.Retention{ExpireAfter: cfg.TranscriptRetention}, nil); err != nil {
		return fmt.Errorf("initialize receipt ledger: %w", err)
	}

	static, err := fs.Sub(overlay, "static")
	if err != nil {
		return err
	}
	if _, err := fs.Stat(static, "index.html"); err != nil {
		return fmt.Errorf("overlay is not built; run ./tools/build-web.sh before building receipts")
	}
	var handler http.Handler = http.FileServer(http.FS(static))
	if cfg.AuthEnabled {
		clientSecret, err := cfg.TwitchClientSecret.Reveal()
		if err != nil {
			return fmt.Errorf("initialize Twitch sign-in configuration")
		}
		encodedKey, err := cfg.TokenEncryptionKey.Reveal()
		if err != nil {
			return fmt.Errorf("initialize token encryption configuration")
		}
		key, err := base64.StdEncoding.DecodeString(encodedKey)
		if err != nil || len(key) != 32 {
			return fmt.Errorf("RECEIPTS_TOKEN_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
		}
		clientID, err := cfg.TwitchClientID.Reveal()
		if err != nil {
			return fmt.Errorf("initialize Twitch sign-in configuration")
		}
		origin := strings.TrimRight(cfg.PublicOrigin, "/")
		authService, err := auth.New(ctx, auth.Config{
			DB: db, Provider: auth.TwitchProvider{ClientID: clientID, ClientSecret: clientSecret},
			ClientID: clientID, RedirectURL: origin + "/auth/twitch/callback", PublicOrigin: origin,
			EncryptionKey: key, SecureCookie: strings.HasPrefix(origin, "https://"),
		})
		if err != nil {
			return err
		}
		mux := http.NewServeMux()
		mux.Handle("/auth/", authService.Handler())
		mux.Handle("/", http.FileServer(http.FS(static)))
		handler = mux
	}
	return serve(cfg.HTTPAddress, handler)
}

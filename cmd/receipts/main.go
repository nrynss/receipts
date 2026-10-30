package main

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/sqlite"
	"receipts/internal/api"
	"receipts/internal/auth"
	"receipts/internal/demo"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/session"
	"receipts/internal/settings"
	"receipts/internal/store"
	"receipts/internal/twitch/connection"
)

//go:embed all:static
var overlay embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "replay" {
		if err := replayCommand(context.Background(), os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
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

	ledger, err := store.New(ctx, db, store.Retention{ExpireAfter: cfg.TranscriptRetention}, nil)
	if err != nil {
		return fmt.Errorf("initialize receipt ledger: %w", err)
	}
	if err := ledger.ResetLiveAuthority(ctx); err != nil {
		return fmt.Errorf("reset stale live authority: %w", err)
	}

	if _, err := ledger.RecoverDueAppeals(ctx); err != nil {
		return fmt.Errorf("recover pending appeals: %w", err)
	}

	static, err := fs.Sub(overlay, "static")
	if err != nil {
		return err
	}
	if _, err := fs.Stat(static, "index.html"); err != nil {
		return fmt.Errorf("overlay is not built; run ./tools/build-web.sh before building receipts")
	}
	demoServer := demo.New("", 64)
	defer demoServer.Close()
	mux := http.NewServeMux()
	mux.Handle("/demo/api/", demoServer)
	assets := http.FileServer(http.FS(static))
	mux.Handle("/", staticFallback(static, assets))
	var handler http.Handler = mux
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
		supervisor := channelruntime.New(channelruntime.Config{MaxChannels: cfg.MaxActiveChannels})
		// Keel stores the explicit operator ceilings in nanodollars in the
		// same SQLite file under its own namespace.
		channelLimit, err := usdPrice(cfg.ChannelSpendLimitUSD)
		if err != nil {
			return fmt.Errorf("invalid channel spend limit: %w", err)
		}
		globalLimit, err := usdPrice(cfg.GlobalSpendLimitUSD)
		if err != nil {
			return fmt.Errorf("invalid global spend limit: %w", err)
		}
		budgets, err := channelruntime.OpenBudgets(ctx, db, channelruntime.BudgetConfig{
			GlobalLimit: globalLimit, ChannelLimit: channelLimit,
		})
		if err != nil {
			return fmt.Errorf("initialize durable channel budgets: %w", err)
		}
		supervisor.SetBudgets(budgets)
		authService.OnDisconnect(supervisor.RevokeAccount)
		maintenanceCtx, cancelMaintenance := context.WithCancel(ctx)
		maintenanceDone := make(chan struct{})
		go func() {
			defer close(maintenanceDone)
			authService.Maintain(maintenanceCtx, func(error) {
				log.Print("auth: scheduled Twitch validation failed")
			})
		}()
		defer func() {
			cancelMaintenance()
			<-maintenanceDone
		}()
		mux := http.NewServeMux()
		mux.Handle("/demo/api/", demoServer)
		mux.Handle("/auth/", authService.Handler())
		adapter := &connection.Adapter{Runtime: supervisor, Resolver: connection.ServiceResolverFunc{Service: authService}, Tokens: authService, LossHandler: authService}
		openAIKey, err := cfg.OpenAIAPIKey.Reveal()
		if err != nil {
			return fmt.Errorf("initialize transcription configuration")
		}
		streamlink, _ := lookup("RECEIPTS_STREAMLINK_BIN")
		capture := &audioAttachment{ledger: ledger, runtime: supervisor, apiKey: openAIKey, streamlink: streamlink}
		liveAuthority := &session.Authority{Adapter: adapter, Ledger: ledger, Runtime: supervisor, Freshness: cfg.LiveStatusFreshness, Root: ctx, AttachAudio: capture.attach, AudioActive: capture.activeFor}
		mux.Handle("/account-api/", api.AccountHandlerWithBackend(authService, supervisor, authService.DisconnectProviderAuthorization, budgets, liveAuthority))
		mux.Handle("/api/", api.NewForOwner(api.Config{
			OwnerReader:   api.LedgerReader{Store: ledger, Mode: string(cfg.Mode)},
			OwnerControls: api.LedgerControls{Store: ledger, Runtime: supervisor, AppealDuration: cfg.AppealDuration, StampCooldown: cfg.StampCooldown},
		}, authService, supervisor))
		assets := http.FileServer(http.FS(static))
		mux.Handle("/", staticFallback(static, assets))
		handler = mux
	}
	return serve(cfg.HTTPAddress, handler)
}

// staticFallback serves generated Svelte assets directly and uses adapter-static's
// SPA fallback page for app routes such as /demo/ that have no emitted file.
func staticFallback(static fs.FS, assets http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if name != "" {
			if _, err := fs.Stat(static, name); err == nil {
				assets.ServeHTTP(w, r)
				return
			}
		}
		fallback := r.Clone(r.Context())
		urlCopy := *r.URL
		urlCopy.Path = "/"
		fallback.URL = &urlCopy
		assets.ServeHTTP(w, fallback)
	})
}

func usdPrice(value float64) (cost.Price, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > 1e6 || value*1e9 > float64(math.MaxInt64) {
		return 0, fmt.Errorf("USD limit is outside the supported finite range")
	}
	price := math.Round(value * 1e9)
	if price <= 0 || price > float64(math.MaxInt64) {
		return 0, fmt.Errorf("USD limit is outside the supported price range")
	}
	return cost.Price(price), nil
}

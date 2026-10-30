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
	"strings"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/sqlite"
	"receipts/internal/api"
	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/settings"
	"receipts/internal/store"
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
		mux.Handle("/auth/", authService.Handler())
		mux.Handle("/api/", api.NewForOwner(api.Config{
			OwnerReader:   api.LedgerReader{Store: ledger, Mode: string(cfg.Mode)},
			OwnerControls: api.LedgerControls{Store: ledger, Runtime: supervisor, AppealDuration: cfg.AppealDuration, StampCooldown: cfg.StampCooldown},
		}, authService, supervisor))
		mux.Handle("/", http.FileServer(http.FS(static)))
		handler = mux
	}
	return serve(cfg.HTTPAddress, handler)
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

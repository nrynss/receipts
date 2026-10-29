package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/settings"
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

	static, err := fs.Sub(overlay, "static")
	if err != nil {
		return err
	}
	if _, err := fs.Stat(static, "index.html"); err != nil {
		return fmt.Errorf("overlay is not built; run ./tools/build-web.sh before building receipts")
	}
	return serve(cfg.HTTPAddress, http.FileServer(http.FS(static)))
}

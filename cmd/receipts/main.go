package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/nrynss/keel/sqlite"
)

//go:embed all:static
var overlay embed.FS

func main() {
	dbPath := os.Getenv("RECEIPTS_DB")
	if dbPath == "" {
		dbPath = filepath.Join("data", "receipts.db")
	}
	db, err := sqlite.Open(context.Background(), sqlite.Config{Path: dbPath})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	static, err := fs.Sub(overlay, "static")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := fs.Stat(static, "index.html"); err != nil {
		log.Fatal("overlay is not built; run ./tools/build-web.sh before building receipts")
	}
	log.Fatal(http.ListenAndServe(":8080", http.FileServer(http.FS(static))))
}

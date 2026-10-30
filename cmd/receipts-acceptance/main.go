//go:build acceptance

// receipts-acceptance is a loopback-only synthetic-account harness for real
// OBS Browser Source pairing. It is excluded from ordinary builds.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/api"
	"receipts/internal/auth"
	"receipts/internal/receipt"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/store"
)

type noNetworkProvider struct{}

func (noNetworkProvider) AuthorizeURL(string, string, []string) string { return "" }
func (noNetworkProvider) ExchangeCode(context.Context, string, string) (auth.Tokens, error) {
	return auth.Tokens{}, errors.New("provider calls disabled in acceptance harness")
}
func (noNetworkProvider) Validate(context.Context, string) (auth.Grant, error) {
	return auth.Grant{}, errors.New("provider calls disabled in acceptance harness")
}
func (noNetworkProvider) Refresh(context.Context, string) (auth.Tokens, error) {
	return auth.Tokens{}, errors.New("provider calls disabled in acceptance harness")
}

func main() {
	addr := flag.String("addr", "127.0.0.1:18082", "loopback HTTPS listen address")
	dbPath := flag.String("db", filepath.Join(os.TempDir(), "receipts-ta3-acceptance.db"), "synthetic acceptance SQLite file")
	staticDir := flag.String("static", "cmd/receipts/static", "built Receipts web asset directory")
	cert := flag.String("cert", "", "trusted localhost TLS certificate (required)")
	key := flag.String("key", "", "localhost TLS private key (required)")
	flag.Parse()
	if *cert == "" || *key == "" {
		fatal("provide a locally trusted localhost certificate with -cert and -key; pairing cookies require HTTPS")
	}
	if !loopbackAddress(*addr) {
		fatal("acceptance server must bind to loopback")
	}
	tempDir, _ := filepath.Abs(os.TempDir())
	databasePath, _ := filepath.Abs(*dbPath)
	if filepath.Dir(databasePath) != tempDir || !strings.HasPrefix(filepath.Base(databasePath), "receipts-ta3-acceptance") {
		fatal("synthetic database must use a receipts-ta3-acceptance* filename directly under the system temporary directory")
	}
	if info, err := os.Lstat(databasePath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		fatal("synthetic database cannot be a symlink")
	}
	if _, err := os.Stat(filepath.Join(*staticDir, "index.html")); err != nil {
		fatal("built web assets are missing; run ./tools/build-web.sh first")
	}
	origin := "https://" + *addr
	ctx := context.Background()
	db, err := sqlite.Open(ctx, sqlite.Config{Path: *dbPath})
	if err != nil {
		fatal("could not open synthetic acceptance database")
	}
	defer db.Close()
	ledger, err := store.New(ctx, db, store.Retention{}, nil)
	if err != nil {
		fatal("could not initialize synthetic acceptance database")
	}
	accountID, err := seedOwner(ctx, db, ledger)
	if err != nil {
		fatal("could not seed synthetic owner")
	}
	// Auth startup validates every connected account through Twitch. Keep this
	// synthetic-only row out of that sweep, then enable local owner/device
	// middleware after the production service is constructed.
	if _, err := db.Writer().ExecContext(ctx, `UPDATE accounts SET status='reconnect_required' WHERE id=?`, accountID); err != nil {
		fatal("could not prepare synthetic owner")
	}
	sessions, err := auth.New(ctx, auth.Config{DB: db, Provider: noNetworkProvider{}, ClientID: "synthetic-acceptance", RedirectURL: origin + "/auth/twitch/callback", PublicOrigin: origin, EncryptionKey: bytes32(0x71), SecureCookie: true})
	if err != nil {
		fatal("could not initialize pairing service: " + err.Error())
	}
	if _, err := db.Writer().ExecContext(ctx, `UPDATE accounts SET status='connected' WHERE id=?`, accountID); err != nil {
		fatal("could not activate synthetic owner")
	}
	var channelID string
	if err := db.Reader().QueryRowContext(ctx, `SELECT channel_id FROM accounts WHERE id=?`, accountID).Scan(&channelID); err != nil {
		fatal("could not load synthetic channel")
	}
	liveSessionID, err := seedOverlay(ctx, db, ledger, channelID)
	if err != nil {
		fatal("could not prepare synthetic overlay")
	}
	ownerToken, csrfToken, err := seedBrowserSession(ctx, db, accountID)
	if err != nil {
		fatal("could not create local owner session")
	}
	supervisor := channelruntime.New(channelruntime.Config{MaxChannels: 1})
	if _, err := supervisor.StartVerified(ctx, auth.ChannelContext{AccountID: accountID, ChannelID: channelID, LiveSessionID: liveSessionID, Generation: 1}, ledger); err != nil {
		fatal("could not activate synthetic overlay")
	}
	mux := http.NewServeMux()
	mux.Handle("/auth/", sessions.Handler())
	mux.Handle("/account-api/", api.AccountHandler(sessions, supervisor, nil, nil))
	mux.Handle("/api/", api.NewForOwner(api.Config{OwnerReader: api.LedgerReader{Store: ledger, Mode: "live"}}, sessions, supervisor))
	mux.HandleFunc("GET /__acceptance/login", func(w http.ResponseWriter, r *http.Request) {
		if !remoteLoopback(r.RemoteAddr) || r.Host != *addr {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: ownerToken, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(6 * time.Hour)})
		http.SetCookie(w, &http.Cookie{Name: auth.CSRFCookie, Value: csrfToken, Path: "/", Secure: true, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(6 * time.Hour)})
		http.Redirect(w, r, "/account", http.StatusSeeOther)
	})
	assets := http.FileServer(http.Dir(*staticDir))
	mux.Handle("/", staticFallback(*staticDir, assets))
	fmt.Printf("Synthetic TA.3 acceptance server: %s\nOwner setup: %s/__acceptance/login\nOBS Browser Source: %s/obs\n", origin, origin, origin)
	fmt.Println("Synthetic account; provider calls disabled. Restart this process to check persistent pairing. Do not share browser cookies or pairing codes.")
	server := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	if err := server.ListenAndServeTLS(*cert, *key); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal("acceptance server stopped")
	}
}

func seedOverlay(ctx context.Context, db *sqlite.DB, ledger *store.Store, channelID string) (string, error) {
	var sessionID string
	err := db.Reader().QueryRowContext(ctx, `SELECT id FROM live_sessions WHERE channel_id=? AND state='live' AND ended_at IS NULL ORDER BY started_at DESC LIMIT 1`, channelID).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		live, startErr := ledger.StartSession(ctx, channelID)
		if startErr != nil {
			return "", startErr
		}
		sessionID = live.ID
	} else if err != nil {
		return "", err
	}
	var count int
	if err := db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM receipts WHERE channel_id=?`, channelID).Scan(&count); err != nil {
		return "", err
	}
	if count == 0 {
		segment := receipt.Segment{ID: "ta3-synthetic-source", SessionID: sessionID, Text: "I will never take that shortcut again.", Start: 0, End: time.Second, Attribution: "broadcaster"}
		if err := ledger.AddSegment(ctx, segment); err != nil {
			return "", err
		}
		evidence := receipt.EvidenceInput{SourceID: segment.ID, Text: segment.Text, Start: segment.Start, End: segment.End, Attribution: segment.Attribution}
		captured, err := ledger.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: channelID, SessionID: sessionID, Claim: "Will never take that shortcut again.", ClippedBy: "synthetic", Evidence: []receipt.EvidenceInput{evidence}})
		if err != nil {
			return "", err
		}
		if _, err = ledger.RecordDetection(ctx, receipt.DetectionInput{ChannelID: channelID, ReceiptID: captured.ID, Evidence: evidence, Outcome: receipt.Stamp}); err != nil {
			return "", err
		}
	}
	return sessionID, nil
}

func staticFallback(root string, assets http.Handler) http.Handler {
	files := http.Dir(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, err := files.Open(r.URL.Path)
		if err == nil {
			info, statErr := file.Stat()
			_ = file.Close()
			if statErr == nil && !info.IsDir() {
				assets.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})
}

func seedOwner(ctx context.Context, db *sqlite.DB, ledger *store.Store) (string, error) {
	const accountID = "ta3-synthetic-owner"
	var channelID string
	err := db.Reader().QueryRowContext(ctx, `SELECT channel_id FROM accounts WHERE id=?`, accountID).Scan(&channelID)
	if err == nil {
		return accountID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	channel, err := ledger.CreateChannel(ctx, "ta3-synthetic-owner", "synthetic-owner")
	if err != nil {
		return "", err
	}
	_, err = db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES(?,'twitch','ta3-synthetic-owner','synthetic-owner',?,X'00',0,'["user:read:chat","user:write:chat","clips:edit"]','connected',?)`, accountID, channel.ID, time.Now().UnixMilli())
	return accountID, err
}

func seedBrowserSession(ctx context.Context, db *sqlite.DB, accountID string) (string, string, error) {
	owner, err := randomValue(32)
	if err != nil {
		return "", "", err
	}
	csrf, err := randomValue(32)
	if err != nil {
		return "", "", err
	}
	ownerHash, csrfHash := sha256.Sum256([]byte(owner)), sha256.Sum256([]byte(csrf))
	now := time.Now().UTC()
	_, err = db.Writer().ExecContext(ctx, `INSERT INTO browser_sessions(token_hash,account_id,csrf_hash,created_at,expires_at) VALUES(?,?,?,?,?)`, ownerHash[:], accountID, csrfHash[:], now.UnixMilli(), now.Add(6*time.Hour).UnixMilli())
	return owner, csrf, err
}

func randomValue(n int) (string, error) {
	value := make([]byte, n)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func bytes32(value byte) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = value
	}
	return key
}

func loopbackAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func remoteLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

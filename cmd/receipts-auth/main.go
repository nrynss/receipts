// receipts-auth is a one-shot local probe setup tool. It never prints tokens.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sync"
	"time"

	"receipts/internal/probeauth"
	"receipts/internal/twitch"
)

func main() {
	clientID := flag.String("client-id", "", "public Twitch client ID")
	output := flag.String("output", ".env", "new private environment file; refuses existing files")
	flag.Parse()
	valid := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	if !valid.MatchString(*clientID) {
		log.Fatal("provide the registered public client ID with -client-id")
	}
	if _, err := os.Lstat(*output); !errors.Is(err, os.ErrNotExist) {
		log.Fatal("output must be a new file; existing credentials will not be overwritten")
	}
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		log.Fatal("could not create OAuth transaction")
	}
	state := hex.EncodeToString(stateBytes)
	params := url.Values{"client_id": {*clientID}, "redirect_uri": {probeauth.Callback}, "response_type": {"token"}, "scope": {"user:read:chat user:write:chat clips:edit"}, "state": {state}, "force_verify": {"true"}}
	done := make(chan struct{})
	var mu sync.Mutex
	completed := false
	save := func(token string) error {
		mu.Lock()
		defer mu.Unlock()
		if completed || !valid.MatchString(token) {
			return errors.New("invalid transaction")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		identity, err := (twitch.Client{ClientID: *clientID, Token: token}).Validate(ctx)
		if err != nil || identity.ClientID != *clientID || identity.Login != "nrynss" || identity.UserID == "" {
			return errors.New("identity mismatch")
		}
		for _, scope := range []string{"user:read:chat", "user:write:chat", "clips:edit"} {
			found := false
			for _, granted := range identity.Scopes {
				found = found || granted == scope
			}
			if !found {
				return errors.New("missing scope")
			}
		}
		if !valid.MatchString(identity.UserID) {
			return errors.New("invalid identity")
		}
		file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return errors.New("cannot create environment file")
		}
		_, writeErr := fmt.Fprintf(file, "TWITCH_CLIENT_ID=%s\nTWITCH_ACCESS_TOKEN=%s\nTWITCH_BROADCASTER_ID=%s\n", *clientID, token, identity.UserID)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(*output)
			return errors.New("environment write failed")
		}
		completed = true
		close(done)
		return nil
	}
	server := &http.Server{Addr: "localhost:18081", Handler: probeauth.Handler(state, save), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 15 * time.Second}
	failures := make(chan error, 1)
	go func() { failures <- server.ListenAndServe() }()
	fmt.Println("Local probe receiver: " + probeauth.Callback)
	fmt.Println("Authorize URL: https://id.twitch.tv/oauth2/authorize?" + params.Encode())
	select {
	case <-done:
		fmt.Println("Validated nrynss probe credentials saved locally; no token printed.")
	case <-time.After(10 * time.Minute):
		fmt.Println("Authorization window expired; receiver stopped.")
	case <-failures:
		log.Fatal("local authorization receiver could not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

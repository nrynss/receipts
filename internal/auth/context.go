package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nrynss/keel/id"
)

// ChannelContext is server-validated owner/device identity plus the live
// session authority bound by the runtime manager. Browser sessions are not
// runtime identities and caller-supplied channel IDs never create one.
type ChannelContext struct {
	AccountID        string
	ChannelID        string
	BrowserSessionID string
	LiveSessionID    string
	Generation       uint64
	DeviceID         string
	Device           bool
}
type channelContextKey struct{}

var ErrNoChannelContext = errors.New("auth: validated channel context required")

func WithChannelContext(ctx context.Context, value ChannelContext) context.Context {
	return context.WithValue(ctx, channelContextKey{}, value)
}
func ChannelFromContext(ctx context.Context) (ChannelContext, error) {
	value, ok := ctx.Value(channelContextKey{}).(ChannelContext)
	if !ok || value.AccountID == "" || value.ChannelID == "" {
		return ChannelContext{}, ErrNoChannelContext
	}
	return value, nil
}

// ChannelMiddleware validates the owner browser session or a persistent,
// read-only OBS credential. Revocation cancels active device HTTP streams.
func (s *Service) ChannelMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account, sessionHash, err := s.authenticate(r)
		value := ChannelContext{}
		requestCtx := r.Context()
		var unwatch func()
		if err == nil {
			value = ChannelContext{AccountID: account.ID, ChannelID: account.ChannelID, BrowserSessionID: hex.EncodeToString(sessionHash)}
			if r.Method != http.MethodGet && r.Method != http.MethodHead && (!s.validOrigin(r) || !s.validCSRF(r, sessionHash)) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodGet && r.URL.Path == "/api/events" {
				var cancel context.CancelFunc
				requestCtx, cancel = context.WithCancel(r.Context())
				unwatch, watchErr := s.watchSession(hex.EncodeToString(sessionHash), cancel)
				if watchErr != nil {
					cancel()
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				defer func() { unwatch(); cancel() }()
				r = r.WithContext(requestCtx)
			}
		} else {
			var cancel context.CancelFunc
			value, requestCtx, cancel, unwatch, err = s.authenticateDevice(r)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			defer func() { unwatch(); cancel() }()
			if r.Method != http.MethodGet || (r.URL.Path != "/api/overlay" && r.URL.Path != "/api/events") {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(WithChannelContext(requestCtx, value)))
	})
}

// IssueDeviceCredential is the TA.3 seam for owner-confirmed pairing. The
// secret is returned once; only its digest is persisted.
func (s *Service) IssueDeviceCredential(ctx context.Context, owner ChannelContext) (string, string, error) {
	if owner.Device || owner.AccountID == "" || owner.ChannelID == "" || owner.BrowserSessionID == "" {
		return "", "", ErrForbidden
	}
	deviceID, err := id.New()
	if err != nil {
		return "", "", err
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}
	credential := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(credential))
	res, err := s.db.Writer().ExecContext(ctx, "INSERT INTO device_sessions(id,account_id,channel_id,token_hash,created_at) SELECT ?,a.id,a.channel_id,?,? FROM accounts a WHERE a.id=? AND a.channel_id=? AND a.status='connected' AND EXISTS(SELECT 1 FROM browser_sessions b WHERE b.token_hash=? AND b.account_id=a.id AND b.revoked_at IS NULL AND b.expires_at>?)", deviceID, hash[:], s.now().UTC().UnixMilli(), owner.AccountID, owner.ChannelID, decodeHex(owner.BrowserSessionID), s.now().UTC().UnixMilli())
	if err != nil {
		return "", "", fmt.Errorf("auth: issue device credential: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", "", ErrUnauthorized
	}
	return deviceID, credential, nil
}

func (s *Service) RevokeDeviceCredential(ctx context.Context, owner ChannelContext, deviceID string) error {
	if owner.Device || owner.AccountID == "" || owner.ChannelID == "" || deviceID == "" {
		return ErrForbidden
	}
	s.deviceMu.Lock()
	defer s.deviceMu.Unlock()
	res, err := s.db.Writer().ExecContext(ctx, "UPDATE device_sessions SET revoked_at=? WHERE id=? AND account_id=? AND channel_id=? AND revoked_at IS NULL AND EXISTS(SELECT 1 FROM browser_sessions b WHERE b.token_hash=? AND b.account_id=? AND b.revoked_at IS NULL AND b.expires_at>?)", s.now().UTC().UnixMilli(), deviceID, owner.AccountID, owner.ChannelID, decodeHex(owner.BrowserSessionID), owner.AccountID, s.now().UTC().UnixMilli())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrUnauthorized
	}
	for _, cancel := range s.deviceStreams[deviceID] {
		cancel()
	}
	delete(s.deviceStreams, deviceID)
	return nil
}

func (s *Service) authenticateDevice(r *http.Request) (ChannelContext, context.Context, context.CancelFunc, func(), error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || len(header) < 8 {
		return ChannelContext{}, nil, nil, nil, ErrUnauthorized
	}
	hash := sha256.Sum256([]byte(strings.TrimPrefix(header, "Bearer ")))
	var value ChannelContext
	err := s.db.Reader().QueryRowContext(r.Context(), "SELECT d.id,a.id,d.channel_id FROM device_sessions d JOIN accounts a ON a.id=d.account_id WHERE d.token_hash=? AND d.revoked_at IS NULL AND a.status='connected'", hash[:]).Scan(&value.DeviceID, &value.AccountID, &value.ChannelID)
	if err != nil {
		return ChannelContext{}, nil, nil, nil, ErrUnauthorized
	}
	value.Device = true
	ctx, cancel := context.WithCancel(r.Context())
	unwatch, err := s.watchDevice(value.DeviceID, cancel)
	if err != nil {
		cancel()
		return ChannelContext{}, nil, nil, nil, err
	}
	return value, ctx, cancel, unwatch, nil
}
func (s *Service) watchDevice(deviceID string, cancel context.CancelFunc) (func(), error) {
	s.deviceMu.Lock()
	defer s.deviceMu.Unlock()
	var active int
	err := s.db.Reader().QueryRowContext(context.Background(), "SELECT count(*) FROM device_sessions WHERE id=? AND revoked_at IS NULL", deviceID).Scan(&active)
	if err != nil || active == 0 {
		return nil, ErrUnauthorized
	}
	s.deviceWatchSeq++
	watchID := s.deviceWatchSeq
	if s.deviceStreams[deviceID] == nil {
		s.deviceStreams[deviceID] = map[uint64]context.CancelFunc{}
	}
	s.deviceStreams[deviceID][watchID] = cancel
	return func() {
		s.deviceMu.Lock()
		delete(s.deviceStreams[deviceID], watchID)
		if len(s.deviceStreams[deviceID]) == 0 {
			delete(s.deviceStreams, deviceID)
		}
		s.deviceMu.Unlock()
	}, nil
}

func (s *Service) watchSession(tokenHash string, cancel context.CancelFunc) (func(), error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	var active int
	err := s.db.Reader().QueryRowContext(context.Background(), "SELECT count(*) FROM browser_sessions WHERE token_hash=? AND revoked_at IS NULL AND expires_at>?", decodeHex(tokenHash), s.now().UTC().UnixMilli()).Scan(&active)
	if err != nil || active == 0 {
		return nil, ErrUnauthorized
	}
	var expiresAt int64
	if err := s.db.Reader().QueryRowContext(context.Background(), "SELECT expires_at FROM browser_sessions WHERE token_hash=? AND revoked_at IS NULL", decodeHex(tokenHash)).Scan(&expiresAt); err != nil {
		return nil, ErrUnauthorized
	}
	expiryTimer := time.AfterFunc(time.UnixMilli(expiresAt).UTC().Sub(s.now().UTC()), cancel)
	s.sessionWatchSeq++
	id := s.sessionWatchSeq
	if s.sessionStreams[tokenHash] == nil {
		s.sessionStreams[tokenHash] = map[uint64]context.CancelFunc{}
	}
	s.sessionStreams[tokenHash][id] = cancel
	return func() {
		expiryTimer.Stop()
		s.sessionMu.Lock()
		delete(s.sessionStreams[tokenHash], id)
		if len(s.sessionStreams[tokenHash]) == 0 {
			delete(s.sessionStreams, tokenHash)
		}
		s.sessionMu.Unlock()
	}, nil
}

func (s *Service) cancelSessionStreams(tokenHash string) {
	s.sessionMu.Lock()
	for _, cancel := range s.sessionStreams[tokenHash] {
		cancel()
	}
	delete(s.sessionStreams, tokenHash)
	s.sessionMu.Unlock()
}

func decodeHex(value string) []byte { raw, _ := hex.DecodeString(value); return raw }

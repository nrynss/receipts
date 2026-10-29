package auth

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
)

// ChannelContext is a server-validated authority. Values from query strings,
// JSON bodies and Twitch chat are never converted to this type.
type ChannelContext struct {
	AccountID string
	ChannelID string
	SessionID string
	DeviceID  string
	Device    bool
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

// ChannelMiddleware derives owner authority from the current opaque browser
// session on every request. A device verifier can be composed by TA.3.
func (s *Service) ChannelMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		account, sessionHash, err := s.authenticate(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !s.validOrigin(r) || !s.validCSRF(r, sessionHash) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		value := ChannelContext{AccountID: account.ID, ChannelID: account.ChannelID, SessionID: hex.EncodeToString(sessionHash)}
		next.ServeHTTP(w, r.WithContext(WithChannelContext(r.Context(), value)))
	})
}

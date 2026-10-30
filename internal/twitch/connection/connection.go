// Package connection binds Twitch requests to one verified channel runtime.
package connection

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"

	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
	"receipts/internal/twitch"
)

var (
	ErrBinding = errors.New("twitch connection: authorization does not match channel")
	ErrLost    = errors.New("twitch connection: authorization lost; reconnect required")
)

// Grant contains persisted, validated authorization metadata, never credentials.
type Grant struct {
	ClientID          string
	SubjectID         string
	ChannelPlatformID string
	Scopes            []string
}

type Resolver interface {
	Resolve(context.Context, auth.ChannelContext) (Grant, error)
}
type Tokens interface {
	AccessToken(context.Context, string) (string, error)
	RefreshAfterUnauthorized(context.Context, string, string) (string, error)
}

type Health struct {
	Connected         bool
	AuthorizationLost bool
	Err               string
}

type Adapter struct {
	Runtime  *channelruntime.Manager
	Resolver Resolver
	Tokens   Tokens
	HTTP     *http.Client
	BaseURL  string
}

// ServiceResolver adapts the hosted auth service's persisted binding lookup.
type ServiceResolver interface {
	AccountForChannel(context.Context, auth.ChannelContext) (auth.ChannelAccount, error)
}
type ServiceResolverFunc struct{ Service ServiceResolver }

func (r ServiceResolverFunc) Resolve(ctx context.Context, authority auth.ChannelContext) (Grant, error) {
	account, err := r.Service.AccountForChannel(ctx, authority)
	if err != nil {
		return Grant{}, err
	}
	return Grant{ClientID: account.ClientID, SubjectID: account.SubjectID, ChannelPlatformID: account.ChannelPlatformID, Scopes: account.Scopes}, nil
}

// Do uses only the authority-bound account and broadcaster. Callers cannot
// provide a credential or select another channel through endpoint payload IDs.
func (a *Adapter) binding(ctx context.Context, authority auth.ChannelContext, method, endpoint string) (context.Context, Grant, string, error) {
	if a == nil || a.Runtime == nil || a.Resolver == nil || a.Tokens == nil {
		return nil, Grant{}, "", errors.New("twitch connection: unavailable")
	}
	run, err := a.Runtime.Resolve(authority)
	if err != nil || run.Context.Err() != nil {
		return nil, Grant{}, "", channelruntime.ErrStale
	}
	g, err := a.Resolver.Resolve(ctx, authority)
	if err != nil {
		return nil, Grant{}, "", err
	}
	wantScope := scopeFor(method, endpoint)
	if authority.AccountID == "" || authority.ChannelID == "" || g.ClientID == "" || g.SubjectID == "" || g.ChannelPlatformID == "" || g.ChannelPlatformID != g.SubjectID || wantScope == "" || (wantScope != "-" && !has(g.Scopes, wantScope)) {
		return nil, Grant{}, "", ErrBinding
	}
	token, err := a.Tokens.AccessToken(ctx, authority.AccountID)
	if err != nil {
		return nil, Grant{}, "", a.failIfLost(authority, classify(err))
	}
	return ctx, g, token, nil
}

func (a *Adapter) request(ctx context.Context, authority auth.ChannelContext, method, endpoint string, query url.Values, body any, out any) error {
	run, err := a.Runtime.Resolve(authority)
	if err != nil {
		return channelruntime.ErrStale
	}
	ctx, cancel := context.WithCancel(ctx)
	stopRuntimeWatch := context.AfterFunc(run.Context, cancel)
	defer func() { stopRuntimeWatch(); cancel() }()
	ctx, g, token, err := a.binding(ctx, authority, method, endpoint)
	if err != nil {
		return err
	}
	client := twitch.Client{ClientID: g.ClientID, Token: token, HTTP: a.HTTP, BaseURL: a.BaseURL}
	err = client.Request(ctx, method, endpoint, query, body, out)
	if !isUnauthorized(err) {
		return err
	}
	newToken, refreshErr := a.Tokens.RefreshAfterUnauthorized(ctx, authority.AccountID, token)
	if refreshErr != nil {
		return a.failIfLost(authority, classify(refreshErr))
	}
	if newToken == "" || newToken == token {
		return a.failIfLost(authority, ErrLost)
	}
	client.Token = newToken
	err = client.Request(ctx, method, endpoint, query, body, out)
	if isUnauthorized(err) {
		return a.failIfLost(authority, ErrLost)
	}
	return err
}

func (a *Adapter) failIfLost(authority auth.ChannelContext, err error) error {
	if errors.Is(err, ErrLost) && a.Runtime != nil {
		_ = a.Runtime.Stop(authority)
	}
	return err
}

func classify(err error) error {
	if errors.Is(err, auth.ErrReconnect) || errors.Is(err, auth.ErrUnauthorized) {
		return errors.Join(ErrLost, err)
	}
	return err
}
func isUnauthorized(err error) bool {
	var provider *twitch.HTTPError
	return errors.As(err, &provider) && provider.StatusCode == http.StatusUnauthorized
}
func has(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}
func scopeFor(method, endpoint string) string {
	switch {
	case endpoint == "/chat/messages" && method == http.MethodPost:
		return "user:write:chat"
	case endpoint == "/clips" && method == http.MethodPost:
		return "clips:edit"
	case endpoint == "/eventsub/subscriptions" && method == http.MethodPost:
		return "user:read:chat"
	case endpoint == "/streams" && method == http.MethodGet:
		return "-"
	default:
		return ""
	}
}

// RequiredScopes is the minimal set needed by the currently supported actions.
func RequiredScopes() []string {
	out := []string{"clips:edit", "user:read:chat", "user:write:chat"}
	sort.Strings(out)
	return out
}

func (a *Adapter) StreamLive(ctx context.Context, authority auth.ChannelContext) (bool, error) {
	_, g, _, err := a.binding(ctx, authority, http.MethodGet, "/streams")
	if err != nil {
		return false, err
	}
	var got struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	err = a.request(ctx, authority, http.MethodGet, "/streams", url.Values{"user_id": {g.ChannelPlatformID}}, nil, &got)
	return len(got.Data) > 0, err
}
func (a *Adapter) SendChatMessage(ctx context.Context, authority auth.ChannelContext, message string) (bool, error) {
	_, g, _, err := a.binding(ctx, authority, http.MethodPost, "/chat/messages")
	if err != nil {
		return false, err
	}
	msg := twitch.ChatMessage{BroadcasterID: g.ChannelPlatformID, SenderID: g.SubjectID, Message: message}
	var got struct {
		Data []struct {
			IsSent bool `json:"is_sent"`
		} `json:"data"`
	}
	err = a.request(ctx, authority, http.MethodPost, "/chat/messages", nil, msg, &got)
	if err == nil && len(got.Data) == 0 {
		return false, errors.New("twitch connection: chat response missing result")
	}
	if err != nil {
		return false, err
	}
	return got.Data[0].IsSent, nil
}
func (a *Adapter) CreateClip(ctx context.Context, authority auth.ChannelContext) (string, error) {
	_, g, _, err := a.binding(ctx, authority, http.MethodPost, "/clips")
	if err != nil {
		return "", err
	}
	var got struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	err = a.request(ctx, authority, http.MethodPost, "/clips", url.Values{"broadcaster_id": {g.ChannelPlatformID}}, nil, &got)
	if err != nil {
		return "", err
	}
	if len(got.Data) == 0 || got.Data[0].ID == "" {
		return "", errors.New("twitch connection: clip response missing ID")
	}
	return got.Data[0].ID, nil
}
func (a *Adapter) SubscribeChat(ctx context.Context, authority auth.ChannelContext, sessionID string) error {
	_, g, _, err := a.binding(ctx, authority, http.MethodPost, "/eventsub/subscriptions")
	if err != nil {
		return err
	}
	body := map[string]any{"type": "channel.chat.message", "version": "1", "condition": map[string]string{"broadcaster_user_id": g.ChannelPlatformID, "user_id": g.SubjectID}, "transport": map[string]string{"method": "websocket", "session_id": sessionID}}
	var got struct {
		Data []struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err = a.request(ctx, authority, http.MethodPost, "/eventsub/subscriptions", nil, body, &got); err != nil {
		return err
	}
	if len(got.Data) == 0 {
		return errors.New("twitch connection: subscription response missing")
	}
	return nil
}

func (a *Adapter) HealthFor(ctx context.Context, authority auth.ChannelContext) Health {
	_, err := a.StreamLive(ctx, authority)
	if err == nil {
		return Health{Connected: true}
	}
	return Health{AuthorizationLost: errors.Is(err, ErrLost), Err: fmt.Sprint(err)}
}

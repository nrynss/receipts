// Package connection binds Twitch requests to one verified channel runtime.
package connection

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

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
type AuthorizationLoss interface {
	RevokeProviderAuthorizationIfCurrent(context.Context, string, string) error
}

type Health struct {
	Connected         bool
	AuthorizationLost bool
	Err               string
}

// StreamStatus is an observational status snapshot, not live-session authority.
type StreamStatus struct {
	Known       bool
	Live        bool
	BroadcastID string
	ObservedAt  time.Time
}

type Adapter struct {
	Runtime     *channelruntime.Manager
	Resolver    Resolver
	Tokens      Tokens
	LossHandler AuthorizationLoss
	HTTP        *http.Client
	BaseURL     string
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

// binding joins caller and runtime cancellation before metadata or token work.
func (a *Adapter) binding(ctx context.Context, authority auth.ChannelContext, method, endpoint string) (context.Context, Grant, string, func(), error) {
	if a == nil || a.Runtime == nil || a.Resolver == nil || a.Tokens == nil {
		return nil, Grant{}, "", func() {}, errors.New("twitch connection: unavailable")
	}
	run, err := a.Runtime.Resolve(authority)
	if err != nil || run.Context.Err() != nil {
		return nil, Grant{}, "", func() {}, channelruntime.ErrStale
	}
	ctx, cancel := context.WithCancel(ctx)
	stopRuntimeWatch := context.AfterFunc(run.Context, cancel)
	done := func() { stopRuntimeWatch(); cancel() }
	if err := ctx.Err(); err != nil {
		done()
		return nil, Grant{}, "", func() {}, err
	}
	g, err := a.Resolver.Resolve(ctx, authority)
	if err != nil {
		done()
		return nil, Grant{}, "", func() {}, err
	}
	if err := ctx.Err(); err != nil {
		done()
		return nil, Grant{}, "", func() {}, err
	}
	wantScope := scopeFor(method, endpoint)
	if authority.AccountID == "" || authority.ChannelID == "" || g.ClientID == "" || g.SubjectID == "" || g.ChannelPlatformID == "" || g.ChannelPlatformID != g.SubjectID || wantScope == "" || (wantScope != "-" && !has(g.Scopes, wantScope)) {
		done()
		return nil, Grant{}, "", func() {}, ErrBinding
	}
	token, err := a.Tokens.AccessToken(ctx, authority.AccountID)
	if err != nil {
		classified := classify(err)
		if errors.Is(classified, ErrLost) {
			if a.Runtime != nil {
				_ = a.Runtime.Cancel(authority)
			}
		}
		done()
		return nil, Grant{}, "", func() {}, classified
	}
	if err := ctx.Err(); err != nil {
		done()
		return nil, Grant{}, "", func() {}, err
	}
	return ctx, g, token, done, nil
}

func (a *Adapter) requestBound(ctx context.Context, authority auth.ChannelContext, g Grant, token, method, endpoint string, query url.Values, body any, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	client := twitch.Client{ClientID: g.ClientID, Token: token, HTTP: a.HTTP, BaseURL: a.BaseURL}
	err := client.Request(ctx, method, endpoint, query, body, out)
	if !isUnauthorized(err) {
		return err
	}
	newToken, refreshErr := a.Tokens.RefreshAfterUnauthorized(ctx, authority.AccountID, token)
	if refreshErr != nil {
		classified := classify(refreshErr)
		if errors.Is(classified, ErrLost) {
			return a.authorizationLost(ctx, authority, token, classified)
		}
		return classified
	}
	if newToken == "" {
		return a.authorizationLost(ctx, authority, token, ErrLost)
	}
	client.Token = newToken
	err = client.Request(ctx, method, endpoint, query, body, out)
	if isUnauthorized(err) {
		return a.authorizationLost(ctx, authority, newToken, ErrLost)
	}
	return err
}

func (a *Adapter) authorizationLost(ctx context.Context, authority auth.ChannelContext, rejectedToken string, err error) error {
	if a.Runtime != nil {
		if _, resolveErr := a.Runtime.Resolve(authority); resolveErr != nil {
			return err
		}
	}
	err = a.persistAuthorizationLost(ctx, authority, rejectedToken, err)
	if a.Runtime != nil {
		_ = a.Runtime.Cancel(authority)
	}
	return err
}

func (a *Adapter) persistAuthorizationLost(ctx context.Context, authority auth.ChannelContext, rejectedToken string, err error) error {
	handler := a.LossHandler
	if handler == nil {
		handler, _ = a.Tokens.(AuthorizationLoss)
	}
	if handler == nil {
		err = errors.Join(err, errors.New("twitch connection: authorization loss handler unavailable"))
	} else if saveErr := handler.RevokeProviderAuthorizationIfCurrent(ctx, authority.AccountID, rejectedToken); saveErr != nil {
		err = errors.Join(err, errors.New("twitch connection: authorization loss could not be persisted"))
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
	case endpoint == "/clips" && method == http.MethodGet:
		return "-"
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
	ctx, g, token, done, err := a.binding(ctx, authority, http.MethodGet, "/streams")
	if err != nil {
		return false, err
	}
	defer done()
	var got struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	err = a.requestBound(ctx, authority, g, token, http.MethodGet, "/streams", url.Values{"user_id": {g.ChannelPlatformID}}, nil, &got)
	return len(got.Data) > 0, err
}
func (a *Adapter) SendChatMessage(ctx context.Context, authority auth.ChannelContext, message string) (bool, error) {
	ctx, g, token, done, err := a.binding(ctx, authority, http.MethodPost, "/chat/messages")
	if err != nil {
		return false, err
	}
	defer done()
	msg := twitch.ChatMessage{BroadcasterID: g.ChannelPlatformID, SenderID: g.SubjectID, Message: message}
	var got struct {
		Data []struct {
			IsSent bool `json:"is_sent"`
		} `json:"data"`
	}
	err = a.requestBound(ctx, authority, g, token, http.MethodPost, "/chat/messages", nil, msg, &got)
	if err == nil && len(got.Data) == 0 {
		return false, errors.New("twitch connection: chat response missing result")
	}
	if err != nil {
		return false, err
	}
	return got.Data[0].IsSent, nil
}
func (a *Adapter) CreateClip(ctx context.Context, authority auth.ChannelContext) (string, error) {
	ctx, g, token, done, err := a.binding(ctx, authority, http.MethodPost, "/clips")
	if err != nil {
		return "", err
	}
	defer done()
	var got struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	err = a.requestBound(ctx, authority, g, token, http.MethodPost, "/clips", url.Values{"broadcaster_id": {g.ChannelPlatformID}}, nil, &got)
	if err != nil {
		return "", err
	}
	if len(got.Data) == 0 || got.Data[0].ID == "" {
		return "", errors.New("twitch connection: clip response missing ID")
	}
	return got.Data[0].ID, nil
}
func (a *Adapter) SubscribeChat(ctx context.Context, authority auth.ChannelContext, sessionID string) error {
	ctx, g, token, done, err := a.binding(ctx, authority, http.MethodPost, "/eventsub/subscriptions")
	if err != nil {
		return err
	}
	defer done()
	body := map[string]any{"type": "channel.chat.message", "version": "1", "condition": map[string]string{"broadcaster_user_id": g.ChannelPlatformID, "user_id": g.SubjectID}, "transport": map[string]string{"method": "websocket", "session_id": sessionID}}
	var got struct {
		Data []struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err = a.requestBound(ctx, authority, g, token, http.MethodPost, "/eventsub/subscriptions", nil, body, &got); err != nil {
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

// ProbeStreamStatus checks one authenticated owner's channel before a runtime
// exists. Its result is an observation only and cannot authorize capture.
func (a *Adapter) ProbeStreamStatus(ctx context.Context, authority auth.ChannelContext) (StreamStatus, error) {
	if a == nil || a.Resolver == nil || a.Tokens == nil {
		return StreamStatus{}, errors.New("twitch connection: unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if authority.AccountID == "" || authority.ChannelID == "" {
		return StreamStatus{}, auth.ErrNoChannelContext
	}
	g, err := a.Resolver.Resolve(ctx, authority)
	if err != nil {
		return StreamStatus{}, err
	}
	if err = ctx.Err(); err != nil {
		return StreamStatus{}, err
	}
	if g.ClientID == "" || g.SubjectID == "" || g.ChannelPlatformID == "" || g.SubjectID != g.ChannelPlatformID {
		return StreamStatus{}, ErrBinding
	}
	token, err := a.Tokens.AccessToken(ctx, authority.AccountID)
	if err != nil {
		err = classify(err)
		return StreamStatus{}, err
	}
	query := url.Values{"user_id": {g.ChannelPlatformID}}
	var got struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	client := twitch.Client{ClientID: g.ClientID, Token: token, HTTP: a.HTTP, BaseURL: a.BaseURL}
	err = client.Request(ctx, http.MethodGet, "/streams", query, nil, &got)
	if isUnauthorized(err) {
		fresh, refreshErr := a.Tokens.RefreshAfterUnauthorized(ctx, authority.AccountID, token)
		if refreshErr != nil {
			err = classify(refreshErr)
			if errors.Is(err, ErrLost) {
				err = a.persistAuthorizationLost(ctx, authority, token, err)
			}
			return StreamStatus{}, err
		}
		if fresh == "" {
			return StreamStatus{}, a.persistAuthorizationLost(ctx, authority, token, ErrLost)
		}
		client.Token = fresh
		err = client.Request(ctx, http.MethodGet, "/streams", query, nil, &got)
		if isUnauthorized(err) {
			return StreamStatus{}, a.persistAuthorizationLost(ctx, authority, fresh, ErrLost)
		}
	}
	if err != nil {
		return StreamStatus{}, err
	}
	status := StreamStatus{Known: true, Live: len(got.Data) > 0, ObservedAt: time.Now().UTC()}
	if status.Live {
		status.BroadcastID = got.Data[0].ID
	}
	return status, nil
}

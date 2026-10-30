// Package auth implements Twitch-owned account sign-in and durable browser
// sessions for the hosted Receipts server.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrAuthorizationLost = errors.New("auth: provider authorization lost")

// Grant is the user authorization returned by a provider. The service checks
// all fields before attaching an account or persisting credentials.
type Grant struct {
	ClientID string
	UserID   string
	Login    string
	Scopes   []string
	Expires  time.Duration
}

type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
}

// Provider hides network details so the state machine is exercised offline.
type Provider interface {
	AuthorizeURL(state, redirectURL string, scopes []string) string
	ExchangeCode(context.Context, string, string) (Tokens, error)
	Validate(context.Context, string) (Grant, error)
	Refresh(context.Context, string) (Tokens, error)
}

// Revoker is optional so existing offline provider implementations need not
// manage remote token teardown.
type Revoker interface {
	Revoke(context.Context, string) error
}

// TwitchProvider implements Twitch's authorization-code, validation, and
// refresh endpoints. BaseURL is intended for local tests; production uses the
// Twitch authorization origin.
type TwitchProvider struct {
	ClientID     string
	ClientSecret string
	BaseURL      string
	HTTPClient   *http.Client
}

func (p TwitchProvider) base() string {
	if p.BaseURL != "" {
		return strings.TrimRight(p.BaseURL, "/")
	}
	return "https://id.twitch.tv"
}

func (p TwitchProvider) client() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (p TwitchProvider) AuthorizeURL(state, redirectURL string, scopes []string) string {
	u, _ := url.Parse(p.base() + "/oauth2/authorize")
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", redirectURL)
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String()
}

func (p TwitchProvider) ExchangeCode(ctx context.Context, code, redirectURL string) (Tokens, error) {
	return p.tokenRequest(ctx, url.Values{
		"client_id": {p.ClientID}, "client_secret": {p.ClientSecret}, "code": {code},
		"grant_type": {"authorization_code"}, "redirect_uri": {redirectURL},
	})
}

func (p TwitchProvider) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	return p.tokenRequest(ctx, url.Values{
		"client_id": {p.ClientID}, "client_secret": {p.ClientSecret},
		"refresh_token": {refreshToken}, "grant_type": {"refresh_token"},
	})
}

// Revoke removes one user access token at Twitch without including provider
// response bodies in errors.
func (p TwitchProvider) Revoke(ctx context.Context, accessToken string) error {
	if p.ClientID == "" || accessToken == "" {
		return errors.New("auth: provider revoke failed")
	}
	form := url.Values{"client_id": {p.ClientID}, "token": {accessToken}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base()+"/oauth2/revoke", strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("auth: provider revoke failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client().Do(req)
	if err != nil {
		return errors.New("auth: provider revoke failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("auth: provider revoke failed")
	}
	return nil
}

func (p TwitchProvider) tokenRequest(ctx context.Context, form url.Values) (Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base()+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, errors.New("auth: token exchange failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client().Do(req)
	if err != nil {
		return Tokens{}, errors.New("auth: token exchange failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusBadRequest {
		return Tokens{}, ErrAuthorizationLost
	}
	if resp.StatusCode != http.StatusOK {
		return Tokens{}, errors.New("auth: token exchange failed")
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil || body.AccessToken == "" || body.RefreshToken == "" || body.ExpiresIn <= 0 {
		return Tokens{}, errors.New("auth: token exchange failed")
	}
	return Tokens{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken, ExpiresIn: time.Duration(body.ExpiresIn) * time.Second}, nil
}

func (p TwitchProvider) Validate(ctx context.Context, accessToken string) (Grant, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.base()+"/oauth2/validate", nil)
	if err != nil {
		return Grant{}, errors.New("auth: token validation failed")
	}
	req.Header.Set("Authorization", "OAuth "+accessToken)
	resp, err := p.client().Do(req)
	if err != nil {
		return Grant{}, errors.New("auth: token validation failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusBadRequest {
		return Grant{}, ErrAuthorizationLost
	}
	if resp.StatusCode != http.StatusOK {
		return Grant{}, errors.New("auth: token validation failed")
	}
	var body struct {
		ClientID string   `json:"client_id"`
		UserID   string   `json:"user_id"`
		Login    string   `json:"login"`
		Scopes   []string `json:"scopes"`
		Expires  int64    `json:"expires_in"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil {
		return Grant{}, errors.New("auth: token validation failed")
	}
	return Grant{ClientID: body.ClientID, UserID: body.UserID, Login: body.Login, Scopes: body.Scopes, Expires: time.Duration(body.Expires) * time.Second}, nil
}

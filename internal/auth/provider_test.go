package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestTwitchProviderAuthorizationExchangeValidationAndRefresh(t *testing.T) {
	var tokenCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Errorf("token request method/header: %s %q", r.Method, r.Header.Get("Content-Type"))
			}
			tokenCalls++
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				if r.Form.Get("code") != "one-time-code" || r.Form.Get("redirect_uri") != "https://receipts.example/auth/twitch/callback" || r.Form.Get("client_secret") != "client-secret" {
					t.Errorf("authorization exchange fields: %#v", r.Form)
				}
				io.WriteString(w, `{"access_token":"access-one","refresh_token":"refresh-one","expires_in":3600}`)
			case "refresh_token":
				if r.Form.Get("refresh_token") != "refresh-one" {
					t.Errorf("refresh fields: %#v", r.Form)
				}
				io.WriteString(w, `{"access_token":"access-two","refresh_token":"refresh-two","expires_in":1800}`)
			default:
				t.Errorf("unexpected grant type %q", r.Form.Get("grant_type"))
				http.Error(w, "bad grant", http.StatusBadRequest)
			}
		case "/oauth2/validate":
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "OAuth access-one" {
				t.Errorf("validation request: method=%s authorization=%q", r.Method, r.Header.Get("Authorization"))
			}
			io.WriteString(w, `{"client_id":"app-1","user_id":"owner-1","login":"owner","scopes":["user:read:chat","user:write:chat","clips:edit"],"expires_in":1200}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p := TwitchProvider{ClientID: "app-1", ClientSecret: "client-secret", BaseURL: server.URL, HTTPClient: server.Client()}

	parsed, err := url.Parse(p.AuthorizeURL("state-random", "https://receipts.example/auth/twitch/callback", DefaultScopes))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Hostname() != "127.0.0.1" || query.Get("response_type") != "code" || query.Get("client_id") != "app-1" || query.Get("state") != "state-random" || query.Get("scope") != strings.Join(DefaultScopes, " ") {
		t.Fatalf("authorize URL = %s", parsed.Redacted())
	}
	tokens, err := p.ExchangeCode(context.Background(), "one-time-code", "https://receipts.example/auth/twitch/callback")
	if err != nil || tokens.AccessToken != "access-one" || tokens.RefreshToken != "refresh-one" {
		t.Fatalf("exchange = %#v, %v", tokens, err)
	}
	grant, err := p.Validate(context.Background(), tokens.AccessToken)
	if err != nil || grant.ClientID != "app-1" || grant.UserID != "owner-1" || !hasScopes(grant.Scopes, DefaultScopes) {
		t.Fatalf("validation = %#v, %v", grant, err)
	}
	rotated, err := p.Refresh(context.Background(), tokens.RefreshToken)
	if err != nil || rotated.AccessToken != "access-two" || rotated.RefreshToken != "refresh-two" || tokenCalls != 2 {
		t.Fatalf("refresh = %#v calls=%d err=%v", rotated, tokenCalls, err)
	}
}

func TestTwitchProviderDoesNotExposeProviderErrorBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"message":"refresh-secret-must-not-leak"}`)
	}))
	defer server.Close()
	p := TwitchProvider{ClientID: "app-1", ClientSecret: "client-secret", BaseURL: server.URL, HTTPClient: server.Client()}
	_, err := p.Refresh(context.Background(), "refresh-secret-must-not-leak")
	if !errors.Is(err, ErrAuthorizationLost) || strings.Contains(err.Error(), "refresh-secret-must-not-leak") {
		t.Fatalf("provider error = %v", err)
	}
}

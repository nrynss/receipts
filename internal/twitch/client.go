package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const helix = "https://api.twitch.tv/helix"

// Client is a deliberately small Helix client used by the P0 probe.
type Client struct {
	ClientID string
	Token    string
	HTTP     *http.Client
	BaseURL  string // Set only by tests; production defaults to Twitch Helix.
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 8 * time.Second}
}

func (c Client) request(ctx context.Context, method, endpoint string, query url.Values, body any, out any) error {
	if c.ClientID == "" || c.Token == "" {
		return errors.New("twitch: client ID and access token are required")
	}
	base := c.BaseURL
	if base == "" {
		base = helix
	}
	u, err := url.Parse(base + endpoint)
	if err != nil {
		return err
	}
	u.RawQuery = query.Encode()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Client-Id", c.ClientID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("twitch: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Never include response bodies or request headers: provider errors can echo sensitive data.
		return fmt.Errorf("twitch: %s %s returned HTTP %d", method, endpoint, resp.StatusCode)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("twitch: decode response: %w", err)
	}
	return nil
}

type ValidateResponse struct {
	ClientID string   `json:"client_id"`
	UserID   string   `json:"user_id"`
	Login    string   `json:"login"`
	Scopes   []string `json:"scopes"`
}

func (c Client) Validate(ctx context.Context) (ValidateResponse, error) {
	// Validate is hosted by id.twitch.tv and is not a Helix endpoint.
	if c.ClientID == "" || c.Token == "" {
		return ValidateResponse{}, errors.New("twitch: client ID and access token are required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://id.twitch.tv/oauth2/validate", nil)
	if err != nil {
		return ValidateResponse{}, err
	}
	req.Header.Set("Authorization", "OAuth "+c.Token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return ValidateResponse{}, fmt.Errorf("twitch: token validation failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ValidateResponse{}, fmt.Errorf("twitch: token validation returned HTTP %d", resp.StatusCode)
	}
	var got ValidateResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&got); err != nil {
		return ValidateResponse{}, fmt.Errorf("twitch: decode token validation: %w", err)
	}
	return got, nil
}

type envelope[T any] struct {
	Data []T `json:"data"`
}
type Stream struct {
	ID string `json:"id"`
}

func (c Client) StreamLive(ctx context.Context, broadcasterID string) (bool, error) {
	var got envelope[Stream]
	if err := c.request(ctx, http.MethodGet, "/streams", url.Values{"user_id": {broadcasterID}}, nil, &got); err != nil {
		return false, err
	}
	return len(got.Data) > 0, nil
}

type ChatMessage struct {
	BroadcasterID string `json:"broadcaster_id"`
	SenderID      string `json:"sender_id"`
	Message       string `json:"message"`
}

type ChatMessageResult struct {
	IsSent bool `json:"is_sent"`
}

func (c Client) SendChatMessage(ctx context.Context, msg ChatMessage) (bool, error) {
	var got envelope[ChatMessageResult]
	if err := c.request(ctx, http.MethodPost, "/chat/messages", nil, msg, &got); err != nil {
		return false, err
	}
	if len(got.Data) == 0 {
		return false, errors.New("twitch: chat response contained no result")
	}
	return got.Data[0].IsSent, nil
}

type ClipCreated struct {
	ID string `json:"id"`
}
type Clip struct {
	ID       string `json:"id"`
	EmbedURL string `json:"embed_url"`
}

func (c Client) CreateClip(ctx context.Context, broadcasterID string) (string, error) {
	var got envelope[ClipCreated]
	if err := c.request(ctx, http.MethodPost, "/clips", url.Values{"broadcaster_id": {broadcasterID}}, nil, &got); err != nil {
		return "", err
	}
	if len(got.Data) == 0 || strings.TrimSpace(got.Data[0].ID) == "" {
		return "", errors.New("twitch: clip creation returned no ID")
	}
	return got.Data[0].ID, nil
}

func (c Client) GetClip(ctx context.Context, clipID string) (Clip, bool, error) {
	var got envelope[Clip]
	if err := c.request(ctx, http.MethodGet, "/clips", url.Values{"id": {clipID}}, nil, &got); err != nil {
		return Clip{}, false, err
	}
	if len(got.Data) == 0 {
		return Clip{}, false, nil
	}
	return got.Data[0], true, nil
}

type SubscriptionResult struct {
	Status string `json:"status"`
}

func (c Client) SubscribeChat(ctx context.Context, broadcasterID, userID, sessionID string) (SubscriptionResult, error) {
	body := map[string]any{
		"type": "channel.chat.message", "version": "1",
		"condition": map[string]string{"broadcaster_user_id": broadcasterID, "user_id": userID},
		"transport": map[string]string{"method": "websocket", "session_id": sessionID},
	}
	var got envelope[SubscriptionResult]
	if err := c.request(ctx, http.MethodPost, "/eventsub/subscriptions", nil, body, &got); err != nil {
		return SubscriptionResult{}, err
	}
	if len(got.Data) == 0 {
		return SubscriptionResult{}, errors.New("twitch: subscription returned no result")
	}
	return got.Data[0], nil
}

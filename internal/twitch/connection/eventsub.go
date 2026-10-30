package connection

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"receipts/internal/auth"
	"receipts/internal/twitch"
)

// SubscribeChatVerified creates the broadcaster-bound chat subscription and
// requires Twitch's response to confirm the subscription is enabled.
func (a *Adapter) SubscribeChatVerified(ctx context.Context, authority auth.ChannelContext, sessionID string) error {
	if sessionID == "" {
		return errors.New("twitch connection: EventSub session required")
	}
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
	err = a.requestBound(ctx, authority, g, token, http.MethodPost, "/eventsub/subscriptions", url.Values{}, body, &got)
	if err != nil {
		var provider *twitch.HTTPError
		if errors.As(err, &provider) && provider.StatusCode == http.StatusForbidden {
			return errors.Join(ErrBinding, err)
		}
		return err
	}
	if len(got.Data) != 1 || got.Data[0].Status != "enabled" {
		if len(got.Data) == 1 && got.Data[0].Status == "authorization_revoked" {
			return ErrLost
		}
		return errors.New("twitch connection: EventSub subscription not enabled")
	}
	return nil
}

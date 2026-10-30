package connection

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"receipts/internal/auth"
)

// ClipReadiness distinguishes an eventual Twitch clip from a still-pending
// response. A not-found response is represented as Ready=false, Err=nil.
type ClipReadiness struct {
	Ready bool
	URL   string
}

// GetClip checks one previously created clip under the same account, runtime,
// generation and cancellation binding as clip creation. An absent clip is
// normal while Twitch processes it.
func (a *Adapter) GetClip(ctx context.Context, authority auth.ChannelContext, clipID string) (ClipReadiness, error) {
	if strings.TrimSpace(clipID) == "" {
		return ClipReadiness{}, errors.New("twitch connection: clip ID required")
	}
	ctx, g, token, done, err := a.binding(ctx, authority, http.MethodPost, "/clips")
	if err != nil {
		return ClipReadiness{}, err
	}
	defer done()
	var got struct {
		Data []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"data"`
	}
	err = a.requestBound(ctx, authority, g, token, http.MethodGet, "/clips", url.Values{"id": {clipID}}, nil, &got)
	if err != nil {
		return ClipReadiness{}, err
	}
	for _, clip := range got.Data {
		if clip.ID != clipID {
			continue
		}
		u, parseErr := url.Parse(clip.URL)
		if parseErr != nil || u.Scheme != "https" || u.Host != "clips.twitch.tv" || u.User != nil || u.Path == "" {
			return ClipReadiness{}, errors.New("twitch connection: invalid clip URL")
		}
		return ClipReadiness{Ready: true, URL: u.String()}, nil
	}
	return ClipReadiness{}, nil
}

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
	if !validClipSlug(clipID) {
		return ClipReadiness{}, errors.New("twitch connection: clip ID required")
	}
	var got struct {
		Data []struct {
			ID            string `json:"id"`
			URL           string `json:"url"`
			BroadcasterID string `json:"broadcaster_id"`
		} `json:"data"`
	}
	ctx, g, token, done, err := a.binding(ctx, authority, http.MethodGet, "/clips")
	if err != nil {
		return ClipReadiness{}, err
	}
	defer done()
	err = a.requestBound(ctx, authority, g, token, http.MethodGet, "/clips", url.Values{"id": {clipID}}, nil, &got)
	if err != nil {
		return ClipReadiness{}, err
	}
	for _, clip := range got.Data {
		if clip.ID != clipID {
			continue
		}
		if clip.BroadcasterID != g.ChannelPlatformID {
			return ClipReadiness{}, errors.New("twitch connection: clip broadcaster does not match channel")
		}
		if !safeClipURL(clip.URL, clipID) {
			return ClipReadiness{}, errors.New("twitch connection: invalid clip URL")
		}
		return ClipReadiness{Ready: true, URL: "https://clips.twitch.tv/" + clipID}, nil
	}
	return ClipReadiness{}, nil
}

func safeClipURL(raw, clipID string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	switch strings.ToLower(u.Hostname()) {
	case "clips.twitch.tv":
		return len(parts) == 1 && parts[0] == clipID
	case "www.twitch.tv":
		return len(parts) == 3 && parts[0] != "" && parts[1] == "clip" && parts[2] == clipID && validChannelSlug(parts[0])
	default:
		return false
	}
}

func validChannelSlug(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func validClipSlug(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

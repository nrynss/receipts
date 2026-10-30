package connection

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"receipts/internal/auth"
	channelruntime "receipts/internal/runtime"
)

func TestGetClipReadinessUsesBoundHelixAndValidatesReturnedURL(t *testing.T) {
	for _, tc := range []struct {
		name, fixture string
		ready         bool
		bad           bool
	}{{"processing", "processing.json", false, false}, {"ready channel URL", "ready.json", true, false}, {"ready clips host URL", "ready-canonical.json", true, false}, {"hostile URL", "hostile-url.json", false, true}, {"wrong broadcaster", "wrong-broadcaster.json", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "twitch", "clips", tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			authz := auth.ChannelContext{AccountID: "owner", ChannelID: "local", LiveSessionID: "session", Generation: 1}
			manager := channelruntime.New(channelruntime.Config{})
			if _, err = manager.Start(context.Background(), authz); err != nil {
				t.Fatal(err)
			}
			adapter := Adapter{Runtime: manager, Resolver: resolverFunc(func(context.Context, auth.ChannelContext) (Grant, error) {
				return Grant{ClientID: "client", SubjectID: "broadcaster", ChannelPlatformID: "broadcaster", Scopes: RequiredScopes()}, nil
			}), Tokens: &tokenFixture{}, BaseURL: "https://unit.invalid", HTTP: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Path != "/clips" || r.URL.Query().Get("id") != "ReceiptsOriginalFixture" {
					t.Errorf("request=%s %s", r.Method, r.URL)
				}
				return response(200, string(body)), nil
			})}}
			got, err := adapter.GetClip(context.Background(), authz, "ReceiptsOriginalFixture")
			if tc.bad {
				if err == nil {
					t.Fatalf("hostile URL accepted: %+v", got)
				}
				return
			}
			if err != nil || got.Ready != tc.ready {
				t.Fatalf("readiness=%+v err=%v", got, err)
			}
			if tc.ready && got.URL != "https://clips.twitch.tv/ReceiptsOriginalFixture" {
				t.Fatalf("URL=%q", got.URL)
			}
		})
	}
}

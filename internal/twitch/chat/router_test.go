package chat

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"receipts/internal/auth"
	"receipts/internal/commands"
	"receipts/internal/ports"
)

type captureFake struct {
	calls     int
	authority commands.Authority
	command   ports.ChatCommand
}

func (f *captureFake) Handle(_ context.Context, a commands.Authority, c ports.ChatCommand) (commands.Result, error) {
	f.calls++
	f.authority = a
	f.command = c
	return commands.Result{}, nil
}

type appealFake struct {
	calls   int
	command ports.ChatCommand
}

func (f *appealFake) HandleChat(_ context.Context, _ auth.ChannelContext, c ports.ChatCommand) error {
	f.calls++
	f.command = c
	return nil
}

type loreFake struct{ calls int }

func (f *loreFake) Handle(context.Context, string, ports.ChatCommand) (string, error) {
	f.calls++
	return "Lore: fine 🏆", nil
}

type senderFake struct {
	calls int
	text  string
}

func (f *senderFake) SendChatMessage(_ context.Context, _ auth.ChannelContext, s string) (bool, error) {
	f.calls++
	f.text = s
	return true, nil
}

func msg(broadcaster, chatter, id, text string, badges ...string) json.RawMessage {
	bs := make([]map[string]string, 0, len(badges))
	for _, b := range badges {
		bs = append(bs, map[string]string{"set_id": b})
	}
	v := map[string]any{"broadcaster_user_id": broadcaster, "chatter_user_id": chatter, "chatter_user_login": "viewer", "message_id": id, "message": map[string]string{"text": text}, "badges": bs}
	b, _ := json.Marshal(v)
	return b
}
func router() (*Router, *captureFake, *appealFake, *loreFake, *senderFake) {
	c, a, l, s := &captureFake{}, &appealFake{}, &loreFake{}, &senderFake{}
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	r := &Router{Authority: auth.ChannelContext{AccountID: "account", ChannelID: "scoped-channel", LiveSessionID: "trusted-session", Generation: 7}, ChannelPlatformID: "broadcaster", Capture: c, Appeals: a, Lore: l, Sender: s, Now: func() time.Time { return now }, DedupeCapacity: 2}
	return r, c, a, l, s
}

func TestRoutesSameReceiptAppealAndLoreServicePortsOnce(t *testing.T) {
	r, c, a, l, s := router()
	ctx := context.Background()
	at := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	if err := r.HandleEvent(ctx, msg("broadcaster", "viewer", "m1", "!receipt"), at); err != nil {
		t.Fatal(err)
	}
	if err := r.HandleEvent(ctx, msg("broadcaster", "viewer", "m1", "!receipt"), at); err != nil {
		t.Fatal(err)
	}
	if c.calls != 1 || c.command.ID != "m1" || c.command.ChannelID != "scoped-channel" || c.command.SessionID != "trusted-session" || c.command.ReceivedAt != at || c.command.Role != ports.ChatViewer {
		t.Fatalf("capture=%+v", c)
	}
	if c.authority != (commands.Authority{ChannelID: "scoped-channel", SessionID: "trusted-session"}) {
		t.Fatalf("authority=%+v", c.authority)
	}
	if err := r.HandleEvent(ctx, msg("broadcaster", "viewer", "m2", "!uphold"), at); err != nil {
		t.Fatal(err)
	}
	if a.calls != 1 || a.command.Role != ports.ChatViewer {
		t.Fatalf("appeal=%+v", a)
	}
	if err := r.HandleEvent(ctx, msg("broadcaster", "viewer", "m3", "!lore"), at); err != nil {
		t.Fatal(err)
	}
	if l.calls != 1 || s.calls != 1 || s.text != "Lore: fine 🏆" {
		t.Fatalf("lore=%d sender=%+v", l.calls, s)
	}
}

func TestRolesFromBroadcasterIdentityAndModeratorBadgeOnly(t *testing.T) {
	r, _, a, _, _ := router()
	at := time.Now().UTC()
	if err := r.HandleEvent(context.Background(), msg("broadcaster", "broadcaster", "m1", "!void x"), at); err != nil {
		t.Fatal(err)
	}
	if a.command.Role != ports.ChatBroadcaster {
		t.Fatalf("broadcaster role=%s", a.command.Role)
	}
	if err := r.HandleEvent(context.Background(), msg("broadcaster", "mod-id", "m2", "!void x", "moderator"), at); err != nil {
		t.Fatal(err)
	}
	if a.command.Role != ports.ChatModerator {
		t.Fatalf("moderator role=%s", a.command.Role)
	}
	if err := r.HandleEvent(context.Background(), msg("broadcaster", "fake-mod", "m3", "!void x"), at); err != nil {
		t.Fatal(err)
	}
	if a.command.Role != ports.ChatViewer {
		t.Fatalf("viewer role=%s", a.command.Role)
	}
}

func TestRejectsForgedChannelOversizeAndBoundsDedupe(t *testing.T) {
	r, c, _, _, _ := router()
	at := time.Now().UTC()
	if err := r.HandleEvent(context.Background(), msg("other", "viewer", "m1", "!receipt"), at); err == nil {
		t.Fatal("forged broadcaster accepted")
	}
	if err := r.HandleEvent(context.Background(), msg("broadcaster", "viewer", "m2", string(make([]byte, MaxMessageBytes+1))), at); err == nil {
		t.Fatal("oversize chat accepted")
	}
	for i := 0; i < 4; i++ {
		if err := r.HandleEvent(context.Background(), msg("broadcaster", "viewer", string(rune('a'+i)), "!receipt"), at); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.seen) > 2 || c.calls != 4 {
		t.Fatalf("dedupe size=%d calls=%d", len(r.seen), c.calls)
	}
}

func TestCommandServiceErrorsDoNotChangeTransportShape(t *testing.T) {
	r, _, _, _, _ := router()
	aErr := errors.New("invalid vote")
	bad := appealError{err: aErr}
	r.Appeals = bad
	if err := r.HandleEvent(context.Background(), msg("broadcaster", "viewer", "m1", "!uphold"), time.Now()); !errors.Is(err, aErr) {
		t.Fatalf("service error=%v", err)
	}
}

type appealError struct{ err error }

func (f appealError) HandleChat(context.Context, auth.ChannelContext, ports.ChatCommand) error {
	return f.err
}

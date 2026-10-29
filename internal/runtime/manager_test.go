package runtime

import (
	"context"
	"errors"
	"testing"

	"receipts/internal/auth"
)

func TestChannelsHaveIndependentAuthorityAndStop(t *testing.T) {
	manager := New(Config{MaxChannels: 2})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", SessionID: "session-a"}
	b := auth.ChannelContext{AccountID: "owner-b", ChannelID: "channel-b", SessionID: "session-b"}
	one, err := manager.Start(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	two, err := manager.Start(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if one.Events == two.Events || one.Epoch == two.Epoch {
		t.Fatal("channels shared publisher or epoch")
	}
	if _, err := manager.Resolve(auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-b", SessionID: "session-b"}); !errors.Is(err, ErrStale) {
		t.Fatalf("guessed channel authority resolved: %v", err)
	}
	if err := manager.Stop(a); err != nil {
		t.Fatal(err)
	}
	if one.Context.Err() == nil {
		t.Fatal("stopped channel context remains active")
	}
	if _, err := manager.Resolve(b); err != nil {
		t.Fatalf("stopping A affected B: %v", err)
	}
}

func TestCapacityAndStaleStop(t *testing.T) {
	manager := New(Config{MaxChannels: 1})
	a := auth.ChannelContext{AccountID: "owner-a", ChannelID: "channel-a", SessionID: "session-a"}
	b := auth.ChannelContext{AccountID: "owner-b", ChannelID: "channel-b", SessionID: "session-b"}
	if _, err := manager.Start(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(context.Background(), b); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity error=%v", err)
	}
	if err := manager.Stop(b); !errors.Is(err, ErrStale) {
		t.Fatalf("cross-channel stop error=%v", err)
	}
}

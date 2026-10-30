package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"receipts/internal/receipt"
)

func TestOwnedLiveAuthorityRolloverResetsMicPermissionAndFencesOwners(t *testing.T) {
	ctx := context.Background()
	s, _, channel, _ := setup(t, Retention{})
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if _, err := s.db.Writer().ExecContext(ctx, `INSERT INTO accounts(id,provider,provider_subject,login,channel_id,token_ciphertext,token_expires_at,scopes,status,updated_at) VALUES('owner','twitch','subject','login',?,X'00',?,'[]','connected',?)`, channel.ID, now.Add(time.Hour).UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	firstActivation, err := s.StartOwnedLiveSession(ctx, "owner", channel.ID, "broadcast-a", now, true)
	if err != nil {
		t.Fatal(err)
	}
	first := firstActivation.Session
	if err = s.VerifyLiveSession(ctx, "owner", channel.ID, first.ID); err != nil {
		t.Fatalf("owner session did not verify: %v", err)
	}
	id, broadcast, checked, mic, generation, err := s.CurrentLiveAuthority(ctx, "owner", channel.ID)
	if err != nil || id != first.ID || broadcast != "broadcast-a" || !mic || !checked.Equal(now) || generation != firstActivation.Generation {
		t.Fatalf("first authority: id=%q broadcast=%q checked=%v mic=%t err=%v", id, broadcast, checked, mic, err)
	}
	segment := segment(t, s, first.ID, "I will finish the challenge", 0)
	receipt, err := s.CreateReceipt(ctx, receipt.ReceiptInput{ChannelID: channel.ID, SessionID: first.ID, Claim: "I will finish the challenge", ClippedBy: "viewer", Evidence: []receipt.EvidenceInput{{SourceID: segment.ID, Text: segment.Text, Start: segment.Start, End: segment.End, Attribution: segment.Attribution}}})
	if err != nil {
		t.Fatal(err)
	}
	appeal, err := s.OpenAppeal(ctx, channel.ID, receipt.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CastVote(ctx, channel.ID, appeal.ID, "voter", "uphold"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartOwnedLiveSession(ctx, "other-owner", channel.ID, "broadcast-b", now.Add(time.Minute), true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign owner activation err=%v", err)
	}
	if err = s.ResetLiveAuthority(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.VerifyLiveSession(ctx, "owner", channel.ID, first.ID); err != nil {
		t.Fatalf("restart discarded ledger session: %v", err)
	}
	resumed, err := s.StartOwnedLiveSession(ctx, "owner", channel.ID, "broadcast-a", now.Add(30*time.Second), false)
	if err != nil || resumed.Session.ID != first.ID || resumed.Generation <= firstActivation.Generation {
		t.Fatalf("same-broadcast restart created a new ledger session or failed to advance generation: %#v err=%v", resumed, err)
	}
	id, broadcast, checked, mic, generation, err = s.CurrentLiveAuthority(ctx, "owner", channel.ID)
	if err != nil || id != first.ID || broadcast != "broadcast-a" || !checked.Equal(now.Add(30*time.Second)) || mic || generation != resumed.Generation {
		t.Fatalf("resumed authority: id=%q broadcast=%q checked=%v mic=%t err=%v", id, broadcast, checked, mic, err)
	}
	activeAppeal, err := s.ActiveAppeal(ctx, channel.ID)
	if err != nil || activeAppeal == nil || activeAppeal.ID != appeal.ID || activeAppeal.Public.UpholdVotes != 1 || !activeAppeal.Public.ClosesAt.Equal(appeal.ClosesAt) {
		t.Fatalf("restart changed appeal history: %#v err=%v", activeAppeal, err)
	}
	failedSame, err := s.StartOwnedLiveSession(ctx, "owner", channel.ID, "broadcast-a", now.Add(45*time.Second), true)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ClearLivePermission(ctx, channel.ID, first.ID, firstActivation.Generation); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old generation cleanup changed resumed session: %v", err)
	}
	if err = s.RollbackLiveActivation(ctx, failedSame); err != nil {
		t.Fatal(err)
	}
	_, _, _, mic, generation, err = s.CurrentLiveAuthority(ctx, "owner", channel.ID)
	if err != nil || mic || generation != resumed.Generation {
		t.Fatalf("same-broadcast rollback lost prior authority: mic=%v generation=%d err=%v", mic, generation, err)
	}
	failedRollover, err := s.StartOwnedLiveSession(ctx, "owner", channel.ID, "broadcast-failed", now.Add(50*time.Second), false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RollbackLiveActivation(ctx, failedRollover); err != nil {
		t.Fatal(err)
	}
	id, broadcast, _, mic, generation, err = s.CurrentLiveAuthority(ctx, "owner", channel.ID)
	if err != nil || id != first.ID || broadcast != "broadcast-a" || mic || generation != resumed.Generation {
		t.Fatalf("changed-broadcast rollback lost prior session: id=%q broadcast=%q mic=%v generation=%d err=%v", id, broadcast, mic, generation, err)
	}
	secondActivation, err := s.StartOwnedLiveSession(ctx, "owner", channel.ID, "broadcast-b", now.Add(time.Minute), false)
	if err != nil {
		t.Fatal(err)
	}
	second := secondActivation.Session
	if err = s.VerifyLiveSession(ctx, "owner", channel.ID, first.ID); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("old session retained authority: %v", err)
	}
	id, broadcast, _, mic, generation, err = s.CurrentLiveAuthority(ctx, "owner", channel.ID)
	if err != nil || id != second.ID || broadcast != "broadcast-b" || mic || generation != secondActivation.Generation {
		t.Fatalf("rollover authority: id=%q broadcast=%q mic=%t err=%v", id, broadcast, mic, err)
	}
	if err = s.TouchLiveAuthority(ctx, "owner", channel.ID, first.ID, "broadcast-a", firstActivation.Generation, now.Add(2*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale update err=%v", err)
	}
}

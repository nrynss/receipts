package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nrynss/keel/sqlite"
	"receipts/internal/receipt"
	"receipts/internal/store"
)

const fixtureClaim = "T61 synthetic restore receipt"

func main() {
	if len(os.Args) < 3 || len(os.Args) > 4 {
		fail("usage: deploy-fixture create|append|verify database-path [claim]")
	}
	ctx := context.Background()
	claim := fixtureClaim
	if len(os.Args) == 4 {
		claim = os.Args[3]
	}
	switch os.Args[1] {
	case "create":
		create(ctx, os.Args[2], claim)
	case "append":
		appendReceipt(ctx, os.Args[2], claim)
	case "verify":
		verify(ctx, os.Args[2], claim)
	default:
		fail("expected create or verify")
	}
}

func create(ctx context.Context, path, claim string) {
	ledger, err := store.Open(ctx, store.Config{Path: path})
	check(err)
	createReceipt(ctx, ledger, "fixture-channel", claim)
	check(ledger.Close())
}

func appendReceipt(ctx context.Context, path, claim string) {
	ledger, err := store.Open(ctx, store.Config{Path: path})
	check(err)
	createReceipt(ctx, ledger, "fixture-current", claim)
	check(ledger.Close())
}

func createReceipt(ctx context.Context, ledger *store.Store, platformID, claim string) {
	channel, err := ledger.CreateChannel(ctx, platformID, "fixture")
	check(err)
	session, err := ledger.StartSession(ctx, channel.ID)
	check(err)
	segment := receipt.Segment{SessionID: session.ID, Start: 0, End: 5 * time.Second, Text: claim, SpeakerID: "fixture-owner", Attribution: "broadcaster"}
	check(ledger.AddSegment(ctx, segment))
	transcript, err := ledger.Transcript(ctx, session.ID)
	check(err)
	if len(transcript) != 1 {
		fail("fixture transcript was not saved")
	}
	_, err = ledger.CreateReceipt(ctx, receipt.ReceiptInput{
		ChannelID: channel.ID, SessionID: session.ID, Claim: claim, ClippedBy: "fixture-viewer",
		Evidence: []receipt.EvidenceInput{{SourceID: transcript[0].ID, Text: transcript[0].Text, Start: transcript[0].Start, End: transcript[0].End, Attribution: transcript[0].Attribution}},
	})
	check(err)
}

func verify(ctx context.Context, path, claim string) {
	db, err := sqlite.Open(ctx, sqlite.Config{Path: path})
	check(err)
	var integrity string
	check(db.Reader().QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity))
	if integrity != "ok" {
		fail("SQLite integrity_check failed")
	}
	var count int
	check(db.Reader().QueryRowContext(ctx, "SELECT count(*) FROM receipts WHERE claim=?", claim).Scan(&count))
	if count != 1 {
		fail(fmt.Sprintf("expected one restored fixture receipt, got %d", count))
	}
	ledger, err := store.New(ctx, db, store.Retention{}, nil)
	check(err)
	rows, err := db.Reader().QueryContext(ctx, "SELECT id FROM channels WHERE platform_id IN ('fixture-channel','fixture-current')")
	check(err)
	channelIDs := []string{}
	for rows.Next() {
		var id string
		check(rows.Scan(&id))
		channelIDs = append(channelIDs, id)
	}
	check(rows.Err())
	check(rows.Close())
	found := false
	for _, channelID := range channelIDs {
		items, err := ledger.PublicReceipts(ctx, channelID, 10)
		check(err)
		for _, item := range items {
			if item.Claim == claim {
				found = true
			}
		}
	}
	if !found {
		fail("restored receipt is not readable through the ledger")
	}
	check(ledger.Close())
	check(db.Close())
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

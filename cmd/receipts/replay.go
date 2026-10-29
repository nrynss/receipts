package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"receipts/internal/replay"
)

// replayCommand is an explicit offline fixture entry point. It reads no
// credentials and opens no provider adapter.
func replayCommand(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("receipts replay", flag.ContinueOnError)
	fixturePath := flags.String("fixture", "", "recorded fixture JSON")
	dbPath := flags.String("db", "", "fresh SQLite ledger path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *fixturePath == "" || *dbPath == "" {
		return fmt.Errorf("fixture and db are required for explicit replay mode")
	}
	if filepath.Clean(*fixturePath) == filepath.Clean(*dbPath) {
		return fmt.Errorf("fixture and db paths must differ")
	}
	if _, err := os.Stat(*dbPath); err == nil {
		return fmt.Errorf("replay database must be fresh")
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.Open(*fixturePath)
	if err != nil {
		return err
	}
	defer f.Close()
	fixture, err := replay.Decode(f)
	if err != nil {
		return err
	}
	clock := replay.NewClock(fixture.Inputs[0].At.Add(-1))
	ledger, err := replay.OpenLedger(ctx, *dbPath, clock)
	if err != nil {
		return err
	}
	defer ledger.Close()
	sinks := replay.Sinks{Transcript: replay.TranscriptSink{Ledger: ledger}}
	if err := fixture.Run(ctx, sinks, clock); err != nil {
		return err
	}
	segments, err := ledger.Store.Transcript(ctx, ledger.Session.ID)
	if err != nil {
		return err
	}
	fmt.Printf("fixture replay complete: persisted_segments=%d; application services not connected\n", len(segments))
	return nil
}

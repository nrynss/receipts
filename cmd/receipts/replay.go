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
	f, err := os.Open(*fixturePath)
	if err != nil {
		return err
	}
	fixture, err := replay.Decode(f)
	_ = f.Close()
	if err != nil {
		return err
	}
	// O_EXCL makes the fresh-path decision atomic across concurrent CLI runs.
	reservation, err := os.OpenFile(*dbPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("replay database must be fresh")
		}
		return err
	}
	reservedInfo, err := reservation.Stat()
	if err != nil {
		_ = reservation.Close()
		return err
	}
	if err := reservation.Close(); err != nil {
		_ = removeOwnedEmpty(*dbPath, reservedInfo)
		return err
	}
	cleanupEmpty := func() { _ = removeOwnedEmpty(*dbPath, reservedInfo) }
	clock := replay.NewClock(fixture.Inputs[0].At.Add(-1))
	ledger, err := replay.OpenLedger(ctx, *dbPath, clock)
	if err != nil {
		cleanupEmpty()
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

func removeOwnedEmpty(path string, reserved os.FileInfo) error {
	current, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(reserved, current) {
		return nil
	}
	if current.Size() != 0 {
		return nil
	}
	return os.Remove(path)
}

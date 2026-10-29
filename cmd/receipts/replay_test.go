package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func writeReplayFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.json")
	data, err := os.ReadFile("../../testdata/replay/foundation.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReplayCommandRequiresFreshDBAndPreservesExistingFile(t *testing.T) {
	fixture := writeReplayFixture(t)
	dbPath := filepath.Join(t.TempDir(), "ledger.db")
	if err := replayCommand(context.Background(), []string{"--fixture", fixture, "--db", dbPath}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("database mode=%#o, want 0600", info.Mode().Perm())
	}
	contentsBefore, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := replayCommand(context.Background(), []string{"--fixture", fixture, "--db", dbPath}); err == nil {
		t.Fatal("existing database was accepted")
	}
	contentsAfter, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contentsAfter) != string(contentsBefore) {
		t.Fatal("existing database changed after rejected replay")
	}
}

func TestReplayCommandAtomicReservationAllowsOnlyOneConcurrentRun(t *testing.T) {
	fixture := writeReplayFixture(t)
	dbPath := filepath.Join(t.TempDir(), "ledger.db")
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- replayCommand(context.Background(), []string{"--fixture", fixture, "--db", dbPath})
		}()
	}
	wg.Wait()
	close(results)
	successes, failures := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("successes=%d failures=%d", successes, failures)
	}
}

func TestReplayCommandRejectsInvalidFixtureBeforeCreatingDB(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(fixture, []byte(`{"version":1,"inputs":[{"at":"2026-01-01T00:00:01Z","kind":"transcript","transcript":{"id":"","session_id":"x","text":"hello","attribution":"wrong"}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "ledger.db")
	if err := replayCommand(context.Background(), []string{"--fixture", fixture, "--db", dbPath}); err == nil {
		t.Fatal("invalid fixture was accepted")
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("invalid fixture reserved DB path: stat err=%v", err)
	}
}

func TestReplayCommandCleansOwnedEmptyDBWhenInitializationFails(t *testing.T) {
	fixture := writeReplayFixture(t)
	dbPath := filepath.Join(t.TempDir(), "ledger.db")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := replayCommand(ctx, []string{"--fixture", fixture, "--db", dbPath}); err == nil {
		t.Fatal("canceled initialization unexpectedly succeeded")
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("owned empty initialization file remains: stat err=%v", err)
	}
}

package audio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStreamBoundsBacklogAndCleans(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "capture")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := streamChunks(ctx, dir, func(ctx context.Context, _ IngestChunk) error { <-ctx.Done(); return ctx.Err() }, func(ctx context.Context) error {
		for i := 0; i < 4; i++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("chunk-%03d.wav", i)), make([]byte, 100), 0600); err != nil {
				return err
			}
		}
		<-ctx.Done()
		return ctx.Err()
	})
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("backlog was not stopped: %v", err)
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("capture directory remains: %v", err)
	}
}
func TestStreamSourceFailureCancelsConsumer(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "capture")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	failure := errors.New("source failure")
	err := streamChunks(ctx, dir, func(context.Context, IngestChunk) error { return nil }, func(context.Context) error { return failure })
	if !errors.Is(err, failure) {
		t.Fatalf("source failure lost: %v", err)
	}
}

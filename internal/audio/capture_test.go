package audio

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nrynss/keel/ffmpeg"
)

func TestReadChunksSortsAndAssignsTimestamps(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"chunk-001.wav", "chunk-000.wav"} {
		if err := os.WriteFile(filepath.Join(dir, name), tinyWAV(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	chunks, err := ReadChunks(dir, started, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || chunks[0].Sequence != 0 || chunks[1].Sequence != 1 {
		t.Fatalf("unexpected chunk order: %#v", chunks)
	}
	if !chunks[0].CapturedAt.Equal(started) || !chunks[1].CapturedAt.Equal(started.Add(5*time.Second)) {
		t.Fatalf("unexpected capture timestamps: %#v", chunks)
	}
}

func tinyWAV() []byte {
	const samples = 800
	data := make([]byte, 44+samples*2)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], 1)
	binary.LittleEndian.PutUint32(data[24:28], 16000)
	binary.LittleEndian.PutUint32(data[28:32], 32000)
	binary.LittleEndian.PutUint16(data[32:34], 2)
	binary.LittleEndian.PutUint16(data[34:36], 16)
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], samples*2)
	return data
}

func TestReadChunksRejectsEmptyChunk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chunk-000.wav")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadChunks(dir, time.Now(), 5*time.Second); err == nil {
		t.Fatal("expected empty chunk to fail")
	}
}

func TestKeelCommandStopsChildOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := ffmpeg.Command(ctx, "sh", "-c", "exec sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cancel()
	started := time.Now()
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected cancelled child to fail")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("cancelled child did not stop promptly")
	}
}

package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// IngestChunk is a complete WAV and an approximate local ingest-clock range.
// It does not establish broadcast or clip time: HLS buffering introduces an
// unknown delay. FirstPCMObserved is when decoded file data was first observed locally.
type IngestChunk struct {
	Sequence         int
	FirstPCMObserved time.Time
	Start, End       time.Time
	WAV              []byte
}

// StreamChunks runs the existing context-bound source and drains only closed
// segments. The directory and queue are bounded even if the consumer stalls.
// It deletes all temporary files when the source stops. The caller must treat
// the times as approximate ingest time, not exact broadcaster speech time.
func StreamChunks(ctx context.Context, streamlink, streamURL, dir string, consume func(context.Context, IngestChunk) error) error {
	return streamChunks(ctx, dir, consume, func(c context.Context) error { return CaptureUntilCanceled(c, streamlink, streamURL, dir) })
}

func streamChunks(ctx context.Context, dir string, consume func(context.Context, IngestChunk) error, capture func(context.Context) error) error {
	if consume == nil {
		return errors.New("audio: consumer required")
	}
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("audio: deadline required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sourceDone := make(chan error, 1)
	go func() { sourceDone <- capture(runCtx) }()
	queue := make(chan IngestChunk, 1)
	consumerDone := make(chan error, 1)
	go func() {
		for {
			select {
			case <-runCtx.Done():
				consumerDone <- runCtx.Err()
				return
			case chunk := <-queue:
				if err := consume(runCtx, chunk); err != nil {
					consumerDone <- err
					return
				}
			}
		}
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var anchor time.Time
	next := 0
	var elapsed time.Duration
	stop := func(err error) error { cancel(); <-sourceDone; <-consumerDone; return err }
	for {
		select {
		case <-ctx.Done():
			return stop(ctx.Err())
		case err := <-consumerDone:
			cancel()
			<-sourceDone
			return fmt.Errorf("audio: consumer stopped: %w", err)
		case err := <-sourceDone:
			cancel()
			<-consumerDone
			return fmt.Errorf("audio: source stopped: %w", err)
		case now := <-ticker.C:
			paths, err := filepath.Glob(filepath.Join(dir, "chunk-*.wav"))
			if err != nil {
				return stop(err)
			}
			sort.Strings(paths)
			if len(paths) > 3 {
				return stop(errors.New("audio: chunk backlog exceeded"))
			}
			var total int64
			for _, path := range paths {
				info, e := os.Stat(path)
				if e != nil {
					return stop(e)
				}
				if now.Sub(info.ModTime()) > 30*time.Second {
					return stop(errors.New("audio: stale chunk backlog"))
				}
				total += info.Size()
				if total > 2<<20 {
					return stop(errors.New("audio: buffer limit exceeded"))
				}
				if anchor.IsZero() && info.Size() > 44 && hasPCMData(path) {
					anchor = now.UTC()
				}
			}
			// ffmpeg opens the next segment only after closing the previous one.
			closed := filepath.Join(dir, fmt.Sprintf("chunk-%03d.wav", next))
			following := filepath.Join(dir, fmt.Sprintf("chunk-%03d.wav", next+1))
			if anchor.IsZero() {
				continue
			}
			if _, err = os.Stat(following); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return stop(err)
			}
			wav, err := os.ReadFile(closed)
			if err != nil {
				return stop(err)
			}
			if len(wav) == 0 || len(wav) > 2<<20 {
				return stop(errors.New("audio: invalid chunk"))
			}
			duration, err := WAVDuration(wav)
			if err != nil || duration > 30*time.Second {
				return stop(errors.New("audio: invalid PCM duration"))
			}
			start := anchor.Add(elapsed)
			if now.Before(start.Add(duration)) {
				continue
			}
			chunk := IngestChunk{Sequence: next, FirstPCMObserved: anchor, Start: start, End: start.Add(duration), WAV: wav}
			select {
			case queue <- chunk:
			default:
				return stop(errors.New("audio: consumer backlog exceeded"))
			}
			if err = os.Remove(closed); err != nil {
				return stop(err)
			}
			elapsed += duration
			next++
		}
	}
}

// hasPCMData observes bytes after the WAV data header, not the header itself.
func hasPCMData(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 4096)
	n, _ := f.Read(b)
	b = b[:n]
	for i := 12; i+8 <= len(b); {
		size := int(binary.LittleEndian.Uint32(b[i+4 : i+8]))
		if string(b[i:i+4]) == "data" {
			return len(b) > i+8
		}
		if size > len(b)-i-8 {
			return false
		}
		i += 8 + size + (size & 1)
	}
	return false
}

package audio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nrynss/keel/ffmpeg"
)

const ChunkDuration = 5 * time.Second

type Chunk struct {
	Sequence   int
	Path       string
	CapturedAt time.Time
	Duration   time.Duration
}

// CaptureStreamlink runs a bounded Streamlink-to-ffmpeg pipeline. Keel's
// context-bound command constructor owns both subprocess lifecycles.
func CaptureStreamlink(ctx context.Context, streamlink, streamURL, outputDir string, duration time.Duration) ([]Chunk, error) {
	if duration < 2*time.Second || duration > 30*time.Second {
		return nil, fmt.Errorf("audio probe duration must be between 2s and 30s")
	}
	started := time.Now().UTC()
	if err := runPipeline(ctx, streamlink, streamURL, outputDir, duration); err != nil {
		return nil, err
	}
	chunks, err := ReadChunks(outputDir, started, ChunkDuration)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("audio probe produced no chunks")
	}
	var observed time.Duration
	for _, chunk := range chunks {
		observed += chunk.Duration
	}
	if observed+time.Second < duration {
		return nil, fmt.Errorf("audio source ended before the requested probe window")
	}
	return chunks, nil
}

// ErrSourceEnded reports that a continuous source closed before cancellation.
var ErrSourceEnded = errors.New("audio source ended")

// CaptureUntilCanceled keeps both subprocesses alive until source EOF or context
// cancellation. A deadline is mandatory; callers must discard partial output.
func CaptureUntilCanceled(ctx context.Context, streamlink, streamURL, outputDir string) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("continuous capture requires a deadline")
	}
	if err := runPipeline(ctx, streamlink, streamURL, outputDir, 0); err != nil {
		return err
	}
	return ErrSourceEnded
}

func runPipeline(ctx context.Context, streamlink, streamURL, outputDir string, duration time.Duration) error {
	if streamlink == "" {
		streamlink = "streamlink"
	}
	if _, err := exec.LookPath(streamlink); err != nil {
		return errors.New("streamlink executable is unavailable")
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return fmt.Errorf("create audio probe directory: %w", err)
	}
	streamCtx, stop := context.WithCancel(ctx)
	defer stop()
	source := ffmpeg.Command(streamCtx, streamlink, "--stdout", streamURL, "best")
	pipe, err := source.StdoutPipe()
	if err != nil {
		return errors.New("open streamlink output failed")
	}
	defer pipe.Close()
	args := []string{"-hide_banner", "-loglevel", "error", "-i", "pipe:0", "-vn", "-ac", "1", "-ar", "16000", "-f", "segment", "-segment_time", strconv.Itoa(int(ChunkDuration.Seconds())), "-reset_timestamps", "1", "-segment_format", "wav"}
	if duration > 0 {
		args = append(args, "-t", strconv.Itoa(int(duration.Seconds())))
	}
	args = append(args, filepath.Join(outputDir, "chunk-%03d.wav"))
	transcode := ffmpeg.Command(streamCtx, "ffmpeg", args...)
	transcode.Stdin = pipe
	if err := source.Start(); err != nil {
		return errors.New("start streamlink failed")
	}
	if err := transcode.Start(); err != nil {
		stop()
		_ = source.Wait()
		return errors.New("start ffmpeg failed")
	}
	transcodeErr := transcode.Wait()
	stop()
	_ = source.Wait()
	if ctx.Err() != nil {
		return fmt.Errorf("audio capture canceled: %w", ctx.Err())
	}
	if transcodeErr != nil {
		return errors.New("ffmpeg capture failed")
	}
	return nil
}

func ReadChunks(dir string, started time.Time, chunkDuration time.Duration) ([]Chunk, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "chunk-*.wav"))
	if err != nil {
		return nil, fmt.Errorf("list audio chunks: %w", err)
	}
	sort.Strings(paths)
	chunks := make([]Chunk, 0, len(paths))
	for _, path := range paths {
		base := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "chunk-"), ".wav")
		sequence, err := strconv.Atoi(base)
		if err != nil || sequence < 0 {
			return nil, fmt.Errorf("invalid audio chunk name %q", filepath.Base(path))
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat audio chunk: %w", err)
		}
		if info.Size() == 0 {
			return nil, fmt.Errorf("audio chunk is empty")
		}
		probeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		duration, err := ffmpeg.Duration(probeCtx, ffmpeg.Tools{}, path)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("probe audio chunk duration: %w", err)
		}
		chunks = append(chunks, Chunk{Sequence: sequence, Path: path, CapturedAt: started.Add(time.Duration(sequence) * chunkDuration), Duration: duration})
	}
	return chunks, nil
}

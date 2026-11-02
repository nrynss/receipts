package transcribe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nrynss/keel/cost"
	"github.com/nrynss/keel/id"
	"receipts/internal/audio"
	"receipts/internal/auth"
	"receipts/internal/receipt"
	runtime "receipts/internal/runtime"
)

// Chunk describes a complete non-overlapping recording on the persisted
// session clock. Start and End bound the audio, not individual word times.
// A source must establish this mapping from its own timing evidence.
type Chunk struct {
	Sequence   uint64
	Start, End time.Duration
	WAV        []byte
}

type Transcriber interface {
	Transcribe(context.Context, []byte) (string, error)
}
type Meter interface {
	Spend(context.Context, auth.ChannelContext, string, string, cost.Price, cost.Work) error
	WithActive(auth.ChannelContext, func(*runtime.Channel) error) error
}
type Ledger interface {
	ClaimAudioChunk(context.Context, string, string, string, uint64, time.Duration, time.Duration) error
	AddLiveSegment(context.Context, string, string, string, uint64, receipt.Segment) error
}

// Worker serializes complete chunks. It never assigns broadcaster identity to
// mixed audio. SoleBroadcaster must be an explicit session-scoped attestation,
// supplied only by a trusted owner authorization path, not microphone consent.
type Worker struct {
	OnSegment       func(receipt.Segment)
	Authority       auth.ChannelContext
	Transcriber     Transcriber
	Meter           Meter
	Ledger          Ledger
	SoleBroadcaster bool
	// Overlap includes the preceding capture chunk in each paid transcription.
	// Capture claims stay disjoint. Persisted windows retain both raw ranges.
	Overlap     bool
	previous    *Chunk
	lastText    string
	lastTextEnd time.Duration
	// NowOffset is the current persisted session-clock offset.
	NowOffset    func() time.Duration
	mu           sync.Mutex
	lastSequence uint64
	lastEnd      time.Duration
	started      bool
}

// Submit accepts only a new contiguous-order, non-overlapping final chunk.
// Overlap mode retains one verified chunk for context and performs no retries.
// The estimate is
// deliberately one full minute at the official gpt-transcribe rate, making
// each <=30 second request conservatively bounded even when interrupted.
func (w *Worker) Submit(ctx context.Context, chunk Chunk) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Transcriber == nil || w.Meter == nil || w.Ledger == nil || w.NowOffset == nil || w.Authority.LiveSessionID == "" || w.Authority.Generation == 0 {
		return errors.New("transcribe: worker unavailable")
	}
	if len(chunk.WAV) == 0 || len(chunk.WAV) > MaxAudioBytes || chunk.Start < 0 || chunk.End <= chunk.Start || chunk.End-chunk.Start > 30*time.Second {
		return errors.New("transcribe: invalid chunk")
	}
	duration, err := audio.WAVDuration(chunk.WAV)
	if err != nil || duration != chunk.End-chunk.Start || chunk.End > w.NowOffset() {
		return errors.New("transcribe: unverified audio duration or future chunk")
	}
	if w.Overlap && duration > 6*time.Second {
		return errors.New("transcribe: overlap capture chunk exceeds six seconds")
	}
	if w.started && (chunk.Sequence <= w.lastSequence || chunk.Start < w.lastEnd) {
		return errors.New("transcribe: duplicate, out-of-order or overlapping chunk")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := w.Ledger.ClaimAudioChunk(ctx, w.Authority.AccountID, w.Authority.ChannelID, w.Authority.LiveSessionID, w.Authority.Generation, chunk.Start, chunk.End); err != nil {
		return err
	}
	// Advance before the paid call, so an ambiguous failure cannot silently
	// replay the same chunk and spend again.
	w.started = true
	w.lastSequence = chunk.Sequence
	w.lastEnd = chunk.End
	wav := chunk.WAV
	segmentStart := chunk.Start
	if w.Overlap {
		if previous := w.previous; previous != nil && previous.End == chunk.Start && previous.Sequence+1 == chunk.Sequence {
			windows, ok := w.Ledger.(interface {
				ClaimTranscriptionWindow(context.Context, string, string, string, uint64, time.Duration, time.Duration, time.Duration) error
			})
			if !ok {
				return errors.New("transcribe: window provenance unavailable")
			}
			wav, err = audio.JoinWAV(previous.WAV, chunk.WAV)
			if err != nil {
				return err
			}
			if err = windows.ClaimTranscriptionWindow(ctx, w.Authority.AccountID, w.Authority.ChannelID, w.Authority.LiveSessionID, w.Authority.Generation, previous.Start, chunk.Start, chunk.End); err != nil {
				return err
			}
			segmentStart = previous.Start
		}
		copyChunk := chunk
		copyChunk.WAV = append([]byte(nil), chunk.WAV...)
		w.previous = &copyChunk
	}
	var text string
	var callErr error
	ref := fmt.Sprintf("audio:%s:%d:%d", w.Authority.LiveSessionID, w.Authority.Generation, chunk.Sequence)
	err = w.Meter.Spend(ctx, w.Authority, "transcription", ref, cost.Price(4_500_000), func(callCtx context.Context) (cost.Usage, error) {
		text, callErr = w.Transcriber.Transcribe(callCtx, wav)
		// Once an HTTP attempt begins, a canceled or failed response can still be
		// billable. Settle the conservative reservation even on an ambiguous error.
		return cost.Usage{}, nil
	})
	if err != nil {
		return err
	}
	if callErr != nil {
		return callErr
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if w.Overlap && segmentStart < w.lastTextEnd && text == w.lastText {
		return nil
	}
	attribution := "uncertain"
	speaker := ""
	if w.SoleBroadcaster {
		attribution = "broadcaster"
		speaker = w.Authority.ChannelID
	}
	segmentID, err := id.New()
	if err != nil {
		return err
	}
	seg := receipt.Segment{ID: segmentID, SessionID: w.Authority.LiveSessionID, Start: segmentStart, End: chunk.End, Text: text, SpeakerID: speaker, Attribution: attribution, TimingBasis: "coarse_pcm_ingest_hls_delay_unknown"}
	err = w.Meter.WithActive(w.Authority, func(_ *runtime.Channel) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return w.Ledger.AddLiveSegment(ctx, w.Authority.AccountID, w.Authority.ChannelID, w.Authority.LiveSessionID, w.Authority.Generation, seg)
	})
	if err == nil {
		w.lastText = text
		w.lastTextEnd = chunk.End
	}
	if err == nil && w.OnSegment != nil && seg.Attribution == "broadcaster" {
		w.OnSegment(seg)
	}
	return err
}

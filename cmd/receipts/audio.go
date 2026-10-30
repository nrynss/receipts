package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"time"

	"receipts/internal/audio"
	"receipts/internal/auth"
	"receipts/internal/receipt"
	runtime "receipts/internal/runtime"
	"receipts/internal/store"
	"receipts/internal/transcribe"
)

var twitchLogin = regexp.MustCompile(`^[A-Za-z0-9_]{1,25}$`)

type audioAttachment struct {
	ledger      *store.Store
	runtime     *runtime.Manager
	apiKey      string
	streamlink  string
	transcriber transcribe.Transcriber
	onSegment   func(auth.ChannelContext, receipt.Segment)
	active      sync.Map
	capture     func(context.Context, string, string, string, func(context.Context, audio.IngestChunk) error) error
}

func (a *audioAttachment) activeFor(identity auth.ChannelContext) bool {
	channel, err := a.runtime.ResolveByIdentity(identity)
	if err != nil {
		return false
	}
	value, ok := a.active.Load(channel.Authority.ChannelID)
	current, valid := value.(captureIdentity)
	return ok && valid && current.session == channel.Authority.LiveSessionID && current.generation == channel.Authority.Generation
}

type captureIdentity struct {
	session    string
	generation uint64
	sole       bool
}

func (a *audioAttachment) transcriptionFor(identity auth.ChannelContext) bool {
	channel, err := a.runtime.ResolveByIdentity(identity)
	if err != nil {
		return false
	}
	value, ok := a.active.Load(identity.ChannelID)
	current, valid := value.(captureIdentity)
	return ok && valid && current.session == channel.Authority.LiveSessionID && current.generation == channel.Authority.Generation && current.sole
}

func (a *audioAttachment) attach(ctx context.Context, authority auth.ChannelContext, sole bool) (func(), error) {
	login, started, err := a.ledger.LiveAudioSource(ctx, authority.AccountID, authority.ChannelID, authority.LiveSessionID, authority.Generation)
	if err != nil {
		return nil, err
	}
	if !twitchLogin.MatchString(login) {
		return nil, errors.New("audio: invalid channel login")
	}
	streamlink := a.streamlink
	if streamlink == "" {
		streamlink = "streamlink"
	}
	if a.capture == nil {
		if _, err := exec.LookPath(streamlink); err != nil {
			return nil, errors.New("audio: streamlink unavailable")
		}
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			return nil, errors.New("audio: ffmpeg unavailable")
		}
	}
	dir, err := os.MkdirTemp("", "receipts-audio-*")
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	done := make(chan struct{})
	transcriber := a.transcriber
	if transcriber == nil {
		transcriber = transcribe.Client{APIKey: a.apiKey}
	}
	worker := transcribe.Worker{OnSegment: func(seg receipt.Segment) {
		if a.onSegment != nil {
			a.onSegment(authority, seg)
		}
	}, Authority: authority, Transcriber: transcriber, Meter: a.runtime, Ledger: a.ledger, SoleBroadcaster: sole, NowOffset: func() time.Duration { return time.Since(started) }}
	go func() {
		defer close(done)
		defer os.RemoveAll(dir)
		defer a.active.CompareAndDelete(authority.ChannelID, captureIdentity{authority.LiveSessionID, authority.Generation, sole})
		capture := a.capture
		if capture == nil {
			capture = audio.StreamChunks
		}
		err := capture(runCtx, streamlink, fmt.Sprintf("https://www.twitch.tv/%s", login), dir, func(ctx context.Context, chunk audio.IngestChunk) error {
			a.active.Store(authority.ChannelID, captureIdentity{authority.LiveSessionID, authority.Generation, sole})
			if !sole {
				return nil
			}
			start, end := chunk.Start.Sub(started), chunk.End.Sub(started)
			if start < 0 {
				return errors.New("audio: unknown session clock")
			}
			return worker.Submit(ctx, transcribe.Chunk{Sequence: uint64(chunk.Sequence), Start: start, End: end, WAV: chunk.WAV})
		})
		if err != nil && ctx.Err() == nil {
			_ = a.runtime.Cancel(authority)
		}
	}()
	return func() { cancel(); <-done }, nil
}

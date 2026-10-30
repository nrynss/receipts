package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"receipts/internal/audio"
	"receipts/internal/auth"
	runtime "receipts/internal/runtime"
	"receipts/internal/store"
	"receipts/internal/transcribe"
)

var twitchLogin = regexp.MustCompile(`^[A-Za-z0-9_]{1,25}$`)

type audioAttachment struct {
	ledger      *store.Store
	runtime     *runtime.Manager
	apiKey      string
	transcriber transcribe.Transcriber
	active      sync.Map
	capture     func(context.Context, string, string, string, func(context.Context, audio.IngestChunk) error) error
}

func (a *audioAttachment) activeFor(identity auth.ChannelContext) bool {
	channel, err := a.runtime.ResolveByIdentity(identity)
	if err != nil {
		return false
	}
	value, ok := a.active.Load(channel.Authority.ChannelID)
	return ok && value == channel.Authority.Generation
}
func (a *audioAttachment) attach(ctx context.Context, authority auth.ChannelContext, sole bool) (func(), error) {
	login, started, err := a.ledger.LiveAudioSource(ctx, authority.AccountID, authority.ChannelID, authority.LiveSessionID, authority.Generation)
	if err != nil {
		return nil, err
	}
	if !twitchLogin.MatchString(login) {
		return nil, errors.New("audio: invalid channel login")
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
	worker := transcribe.Worker{Authority: authority, Transcriber: transcriber, Meter: a.runtime, Ledger: a.ledger, SoleBroadcaster: sole, NowOffset: func() time.Duration { return time.Since(started) }}
	go func() {
		defer close(done)
		defer os.RemoveAll(dir)
		defer a.active.CompareAndDelete(authority.ChannelID, authority.Generation)
		capture := a.capture
		if capture == nil {
			capture = audio.StreamChunks
		}
		err := capture(runCtx, "", fmt.Sprintf("https://www.twitch.tv/%s", login), dir, func(ctx context.Context, chunk audio.IngestChunk) error {
			a.active.Store(authority.ChannelID, authority.Generation)
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

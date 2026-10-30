# T4.4 live-session authority handoff

## Activation contract

The hosted account API asks Twitch for the signed-in broadcaster's current
stream and uses the returned stream ID as the broadcast identity. A start
request creates or resumes an owner-bound SQLite `live_sessions` row only after
a known, fresh, live response. The row stores the Twitch broadcast ID, last
confirmed status time, per-session microphone permission, and a persisted
runtime generation. A same-broadcast restart keeps its ledger session ID and
increments its runtime generation. Runtime activation rechecks the owner,
channel, session, and generation through `StartVerified`.

Every activation receives a new runtime generation. A stop request invalidates
pending starts as well as active runtime work. The runtime polls Twitch status
and a separate freshness watchdog cancels work when confirmation expires.
Offline and changed-broadcast results end the persisted live session; unknown,
stale, and revoked results cancel the captured runtime and clear its freshness
and microphone permission. A fresh confirmation of the same broadcast can
resume the existing ledger session with a new runtime generation, preserving
receipt and appeal history. Delayed work cannot cancel a replacement. Process
startup increments live-session generations and clears stale freshness and
microphone consent without ending the ledger session. Capacity or activation
failure rolls back only its candidate generation and preserves prior session
and appeal data.

Microphone permission defaults to false. The owner may explicitly include
`{"microphone_permission":true}` in a start request after fresh broadcast
confirmation. Permission is cleared on runtime exit, manual stop, stream loss,
rollover, and restart; resuming a same-broadcast ledger session requires a new
explicit choice. There is no microphone capture adapter connected yet.

## Readiness boundary

The API and dock distinguish lifecycle from audio: active verified sessions
report `session_active` and `audio_capture_active:false`. T4.4 does not claim
that audio is captured, transcribed, or sent to a model. The optional activation
attachment hook can connect runtime-bound workers, but the production hook is
not supplied yet: EventSub needs a chat sink backed by the claim capture and
extractor path, and the clip worker needs an eligible persisted receipt. Those
inputs are not connected until the audio/transcription pipeline is ready. No
clip worker is attached by this change.

The status probe follows Twitch's documented Get Streams endpoint, which
returns the live stream records used for the broadcast identity. The Twitch
reference also requires a user or app access token for that API. See the
[Twitch API reference](https://dev.twitch.tv/docs/api/reference).

## Verification

- Focused race checks passed for `internal/session`, `internal/store`,
  `internal/runtime`, `internal/api`, and `internal/twitch/connection`.
- `go vet ./...` passed.
- `GOCACHE=/tmp/receipts-go-cache ./tools/check.sh` passed after linking the
  already-installed web dependencies from the source workspace.
- No live Twitch request, chat post, clip creation, model call, or audio
  recording was made for this implementation.

## Live acceptance

The principal ran `TestLiveAuthorityStopsCapture` against nrynss. The production authority lifecycle activated an isolated synthetic account session and attached the existing real Streamlink/ffmpeg capture probe. At least one second of decoded audio sample data was observed before the owner was asked to stop the stream. Twitch then confirmed the broadcast ended, and the capture subprocesses exited. The run passed in 41.38 seconds with 10 Twitch HTTP requests, no model calls, no chat or clip writes, and discarded temporary audio.

This proves the bounded real stream-end lifecycle rehearsal. It does not connect production audio/transcription or the hosted chat/clip worker composition; those remain downstream integration. See [sanitized authority evidence](evidence/t44-authority.json).

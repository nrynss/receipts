# Audio source probe

## Source decision

Broadcast capture via Streamlink remains the preferred probe target because Receipts must use the published stream as the default source. The documented CLI can write a stream to standard output; ad transitions can pause output and produce discontinuities. That makes chunk continuity an empirical requirement. Keel's `ffmpeg.Command` provides context-bound subprocess execution and bounded shutdown behavior; the probe pipes Streamlink to ffmpeg and emits five-second mono 16 kHz WAV chunks. Twitch Helix Get Streams is checked before and after capture; a lost or ended session invalidates and discards the sample.

The OBS microphone dock remains a fallback only. It must be explicitly enabled per confirmed live session, visibly show listening state, stop immediately on request or loss of live confirmation, discard queued audio, and never auto-resume. Mixed guests, quoted speech, and media are not attributable to the streamer from a mixed broadcast alone and must abstain or enter review.

## Live evidence and limitation

`internal/audio/probe_live_test.go` implements one explicit 5–30 second Streamlink capture probe for nrynss. Enable it with `RECEIPTS_LIVE_PROBE=1` and provide `TWITCH_CLIENT_ID`, `TWITCH_ACCESS_TOKEN`, and `TWITCH_BROADCASTER_ID`; optionally set `RECEIPTS_STREAMLINK_BIN`, `RECEIPTS_PROBE_SECONDS`, and `RECEIPTS_AUDIO_PROBE_OUTPUT`. Run `go test -tags live ./internal/audio -run '^TestLiveStreamlinkCapture$' -count=1`. The probe checks the broadcaster token login and live status before and after capture, cancels both child processes at the requested limit, validates chunks with Keel ffprobe, and deletes the audio when the test ends. Optional evidence contains only chunk sizes, durations, and provisional timestamps and is written mode 0600.

A real short live capture now verifies startup, two measured chunks and bounded child-process release. Controlled pipeline interruption, a fresh source reconnect and actual stream end are now verified in the lifecycle result below. Ad transitions and hours-long stability remain outside this bounded probe. Chunk timestamps are estimates relative to capture startup, not Twitch broadcast timestamps. Initial and final live-status checks do not prove immediate stop on mid-capture status loss. The bounded P0 source lifecycle sequence has now passed; timestamp alignment and broader recovery remain later implementation work. The probe requires Streamlink, ffmpeg, and ffprobe installed on PATH (or a configured Streamlink binary). Synthetic transcript fixtures are not audio-source acceptance evidence.

## Sources

- [Streamlink command line output](https://streamlink.github.io/cli.html)
- [Streamlink Twitch behavior](https://streamlink.github.io/cli/plugins/twitch.html)
- [Keel ffmpeg package v0.9.0](https://pkg.go.dev/github.com/nrynss/keel/ffmpeg)
- [Twitch Get Streams](https://dev.twitch.tv/docs/api/reference/#get-streams)

## Live capture result

The bounded nrynss live capture passed. Keel-managed ffmpeg produced two mono 16 kHz chunks measuring 5.012 and 4.988 seconds, for exactly ten seconds of audio. Helix reported live before and after. Both subprocesses exited at the capture limit and temporary audio was deleted. Sanitized measurements are in `docs/evidence/p0-audio.json`; no broadcast audio is committed. This established short capture and bounded shutdown; the later lifecycle result below adds controlled reconnect and actual stream end. Startup-relative timestamps still require alignment work before claim evidence uses them.

## Offline startup check

After the owner ended the broadcast, the live capture probe confirmed Helix reports nrynss offline and refused startup before creating a capture directory or launching Streamlink/ffmpeg. Evidence is in `docs/evidence/p0-offline.json`. This verifies offline startup refusal, not continuous in-session shutdown or reconnect behavior; those cases remain open. No model calls occurred.

## Active shutdown and reconnect probe

Run `go test -tags live ./internal/audio -run '^TestLiveCaptureLifecycle$' -count=1 -timeout 9m` with explicit `RECEIPTS_LIVE_PROBE=1`, the private Twitch environment and optional `RECEIPTS_STREAMLINK_BIN`/`RECEIPTS_LIFECYCLE_PROBE_OUTPUT`. Budget: eight minutes, at most 240 Helix live checks, two source connections, no chat/clip writes and no model spend. All captured audio is temporary and deleted.

The probe validates nrynss, waits for a live broadcast and observes actual audio bytes before cancelling the first pipeline. It waits for both children to exit, discards partial audio, confirms live status again and opens a fresh source connection. Only after audio resumes does it print `RECONNECTED_CAPTURE_ACTIVE`; the owner should then end the actual broadcast. During capture, status is checked every two seconds. Offline or failed confirmation cancels capture and waits for child cleanup. The probe accepts source EOF followed by authoritative offline confirmation as a distinct termination order. Evidence measures cleanup after the observed Twitch status, not latency from the OBS stop click.

This tests controlled pipeline interruption and a fresh source connection. It does not establish every form of in-process Streamlink retry, ad transition or hours-long stability. Offline/unknown status never automatically resumes a capture session. Deterministic offline tests cover loss of confirmation and the cleanup ordering; real results are recorded only when the complete live sequence succeeds.

## Active lifecycle result

`TestLiveCaptureLifecycle` passed against the actual nrynss stream in 53.77 seconds. Audio bytes were observed before cancelling the first source. Both pipeline children were reaped in 6.610 ms, partial audio was discarded, and a fresh connection produced audio again. The probe then stayed active while the owner ended the broadcast. The source closed and capture finished before Helix's later offline confirmation; it did not merely refuse a new startup. Twenty-six status checks were used, no model calls occurred, and all audio was deleted. Evidence is in `docs/evidence/p0-audio-lifecycle.json`.

`offline_cleanup_ms: 0` means the source had already ended when offline was confirmed; it is not a claim of zero latency from the OBS stop click. Real source EOF was observed. The alternate offline/unknown-status cancellation paths are tested with deterministic checks, and actual process cancellation was exercised by the controlled interruption. In-process automatic network retry and ad recovery are not claimed.

An initial attempt saw live status followed by offline before audio readiness and stopped safely. The final harness requires two consecutive live observations before starting. Cancellation during a status request preserves the cancellation reason instead of mislabeling it as unknown status. The full offline gate and audio race tests pass.

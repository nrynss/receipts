# P0: Ground and feasibility

**Goal:** Prove the risky integration assumptions and establish an offline development gate before the product depends on them.

**Status:** complete

Read [the phase index](README.md), [product spec](../product.md) and [agent protocol](../AGENTS.md) first. Task-level dependencies below are binding.

### T0.1: Build footing

```yaml
requires: []
fixture-ok: yes
owns: go.mod, go.sum, cmd/receipts/, web/, tools/check.sh, .gitignore, .env.example
status: done
```

Initialize Go 1.27 with the pinned Keel release (currently v0.9.0) and Svelte 5 with Chaaya 0.5.0. Inspect the published libraries before choosing wrappers. Serve a minimal overlay from the binary. Ignore secret files and generated artifacts. Establish the required Go and overlay checks; fixture mode needs no credentials. New dependencies need a concrete reason.

**Done when:** A fresh checkout can install pinned dependencies, then build and run the gate without keys, a network connection during checks, or a live stream.

### T0.2: Twitch capability probe

```yaml
requires: T0.1
fixture-ok: no
owns: internal/twitch/, testdata/twitch/, docs/integrations/twitch.md
status: done
```

Check current official Twitch documentation for the exact authorization flow, required scopes, chat subscriptions, clip permissions and live-status signals. Use our own channel for one bounded probe: receive a chat command, reply, create a clip and observe readiness. Check playback from the intended OBS browser origin, including parent and autoplay behavior. Record redacted frames and replies only. Document clip latency and failure behavior rather than assuming the product table is an API contract.

**Done when:** A reproducible live-tagged probe and sanitized evidence establish chat, clip creation and playback behavior. Missing credentials or external restrictions remain explicit blockers; fixture success is not live proof.

### T0.3: Audio source probe

```yaml
requires: T0.1
fixture-ok: no
owns: internal/audio/, testdata/audio/, docs/integrations/audio.md
status: done
```

Probe streamlink and Keel-managed ffmpeg against our own broadcast: startup, chunk boundaries, interruption, reconnect and cancellation. Check Twitch live-status freshness. If broadcast capture is unreliable, prove the OBS mic dock with explicit per-session enablement, visible state, immediate stop and no automatic resume. Define how speaker attribution works; mixed guests or media must not silently become streamer speech. Record the chosen source and its limitations.

**Done when:** One source supplies usable timestamped chunks and releases subprocesses on stop. Evidence covers stream end and lost live confirmation. The mic fallback cannot send queued audio after the session ends.

### T0.4: Evaluation contract and original fixtures

```yaml
requires: T0.1
fixture-ok: yes
owns: testdata/eval/, testdata/transcripts/, docs/evaluation.md
status: done
```

Write an original spoken demo with a promise, unrelated speech, a later contradiction and an appeal. Label at least 40 claim/later-line pairs spanning contradictions, near misses, negation, quotations, changed context, sensitive topics and uncertain speakers. Define eligible explicit spoken promises first; predictions and bets require labelled support before widening. Keep a held-out partition separate from threshold tuning. Define false-stamp rate, precision, recall and abstention with denominators.

**Done when:** Fixtures have stable IDs, labels, source ownership and expected outcomes. Split membership is fixed before tuning. Sensitive and unattributed cases expect suppression or review, never an automatic stamp.

## Exit criteria

- [x] Every task above has evidence against its done criteria.
- [x] The offline gate passes on the candidate revision.
- [x] Live checks are separately identified, budgeted and never part of the offline gate.
- [x] Outstanding limitations and downstream contract changes are recorded.

## Handoff log

### What exists now

T0.1 established Go 1.27 with Keel v0.7.3 (subsequently upgraded to v0.9.0 in `142c2a3`), a Svelte 5/Chaaya 0.5.0 scaffold with locked dependencies, and an offline gate. The Svelte build is embedded into the Go binary rather than duplicated by hand. Installation succeeded with authorized network access. Lint, type checking, overlay tests/build, Go formatting/vet/tests and binary build pass. Packaged-server smoke verified the index, eight embedded assets and SQLite creation. Setup and dependency reasons are in `docs/development.md`.

T0.2 has a runnable live-tagged probe and passing offline Helix/EventSub tests. The selected channel is nrynss. The probe validates that broadcaster and app, receives an explicit command, sends one reply and creates/polls one clip within bounded calls and a 90-second deadline. Twitch registration, authorization, live command receive/reply, clip readiness and local iframe playback are verified. The owner confirmed actual OBS video, audio, autoplay and click-to-play at localhost; deployed-origin playback will be rechecked in P4/P6; see `docs/integrations/twitch.md`.

T0.3 has a runnable live-tagged Streamlink/Keel ffmpeg capture probe with offline WAV/chunk and cancellation tests. Streamlink 8.6.2 is installed in the isolated temporary environment at `/tmp/receipts-audio-probe/bin/streamlink`; ffmpeg/ffprobe are available. The probe restricts capture to nrynss, checks live status before/after, measures chunks, rejects short captures and removes captured audio. Real short capture, measured chunks and bounded subprocess cleanup pass. Controlled interruption/reconnect and actual stream end now pass; offline/unknown confirmation cancels capture in deterministic tests, with actual process cancellation exercised live; see `docs/integrations/audio.md`.

T0.4 is done: `docs/evaluation.md`, `testdata/eval/pairs.jsonl`, and `testdata/transcripts/demo.txt` define an original spoken script and 44 labelled pairs (34 development, 10 held-out). Initial human-style label review made ambiguous development pairs conservative before model tuning; held-out labels are unchanged. The corpus validation test passes. No model threshold or performance metric is claimed.

### Evidence and decisions

T0.1: baseline `c7682b0`; `GOCACHE=/tmp/receipts-go-cache ./tools/check.sh` passes. A bounded local-server smoke starts `bin/receipts serve`, fetches the generated index and eight assets, verifies a temporary SQLite store and stops the process. No live Twitch or audio result is implied. Compatible patched Vite/Vitest clear the installation audit high/moderate reports; remaining low-severity dependency limitations are documented in `docs/development.md`.

T0.2: `go test ./internal/twitch` passes without network or local sockets. The tagged probe compiles and skips unless explicitly enabled. Live chat/clip/local playback results are recorded in `docs/evidence/p0-twitch.json`; Owner-reported OBS acceptance at localhost now passes; production-origin verification remains P4/P6 work.

T0.3: `go test ./internal/audio` passes with generated original WAVs, measured durations and subprocess cancellation. The tagged audio probe compiles and skips unless explicitly enabled. Provisional chunk timestamps are not verified Twitch broadcast timestamps. Real short capture measurements are recorded in `docs/evidence/p0-audio.json`.

T0.4: corpus and contract at `docs/evaluation.md`, `testdata/eval/pairs.jsonl`, and `testdata/transcripts/demo.txt`; `go test ./...` passes fixture integrity checks.

### Notes for the next task

T0.1 no longer has an installation blocker. T0.2 now has a registered app and validated private broadcaster token. Live chat, clip readiness and owner-reported OBS playback at localhost now pass. T0.3 now passes short capture, controlled interruption/reconnect and actual stream end, with deterministic status-loss cancellation tests. No product contract has been changed. P0 is complete; P1 foundation has since landed. T2.1, TA.2 and T3.1 are the next ready tasks.

### Live probe handoff

- Twitch authorization, EventSub receive, bot reply and clip creation/readiness passed on nrynss. Clip readiness latency was 4.898 seconds. Local iframe click-to-play completed the 30.021-second clip; the owner subsequently confirmed OBS video, audio, autoplay and click-to-play at localhost, closing T0.2. Deployed-origin playback remains a later environment check.
- Broadcast audio capture passed with two chunks totalling ten seconds and automatic subprocess cleanup/audio deletion. The later active lifecycle run closes the controlled interruption/reconnect and actual stream-end sequence; T0.3 is now done.
- Evidence: `docs/evidence/p0-twitch.json` and `docs/evidence/p0-audio.json`. No model calls or paid model spend occurred. Media is excluded because the test broadcast showed third-party artwork.
- The manual trigger run timed out without writes; a bounded opt-in self-trigger run then succeeded. The self-trigger path adds one public command message to the one reply and one clip budget.

Offline follow-up: after the owner ended the broadcast, Twitch reported nrynss offline and the audio probe refused before capture startup. Evidence: `docs/evidence/p0-offline.json`. The later active lifecycle run below adds controlled reconnect and real source shutdown.

### Active lifecycle close

T0.3 is complete for the bounded feasibility scope. `TestLiveCaptureLifecycle` observed audio, interrupted and reaped both children in 6.610 ms, obtained audio through a fresh source connection, and remained active through the real broadcast end. Source EOF occurred before Helix confirmed offline; the evidence records both observations distinctly. All temporary audio was deleted and model spend was zero. Offline/unknown status cancellation and cancellation-request races are covered by deterministic tests. Full gate and `go test -race ./internal/audio` pass. Evidence: `docs/evidence/p0-audio-lifecycle.json`.

The owner confirmed OBS video, audio, autoplay and click-to-play at localhost. T0.2 and P0 are complete (4/4 tasks). Deployed-origin playback remains an explicit P4/P6 verification requirement. In-process ad/network recovery and transcript timestamp alignment are not established by the controlled reconnect test.

### P0 close

All four tasks are complete. The owner confirmed video and audio in an actual OBS Browser Source using `http://localhost:18082`, and explicitly confirmed autoplay worked; clicking Play was also exercised. These are owner-reported observations, recorded in `docs/evidence/p0-twitch.json`, separate from the agent-observed in-app iframe playback. The latest full offline gate and audio race suite passed. No media from the third-party-artwork test clip is included in submission assets. P1 foundation has since landed. Recheck clip parent/autoplay behavior at the deployed origin in P4/P6.

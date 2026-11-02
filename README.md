# Receipts

**The stream remembers what you said.** Built with OpenAI Codex.

Chat types `!receipt` when a streamer promises something on stream. Receipts clips the moment and keeps the claim. When the streamer later goes back on it, a **RECEIPT** stamp lands on the overlay with the old clip, and chat votes on the appeal. `!lore` gives new viewers the channel's running promises and the ones that broke.

> **Status:** entry for the Codex × TwitchCon Hackathon, in development. P0 feasibility, P1 foundation and the offline receipt loop are complete. Account isolation, onboarding/OBS pairing and owner controls are implemented. Verified live-session lifecycle and Twitch adapter checks pass. Production text intelligence now uses GPT-6 Luna for extraction and Decisions sensitivity/judging; transcription and embeddings remain unchanged. Contradictions still route to private review and automatic stamps remain disabled; see the [model migration and evaluation limits](docs/model-migration.md) and [development diary](dev-diary/README.md).

Implementation phases, task dependencies and acceptance criteria are in the [development diary](dev-diary/README.md).
Build and run the current server using the [development instructions](docs/development.md).
Hosted setup, backup and recovery are documented in the [operations guide](docs/operations.md).

## For judges: start here

| Try or inspect | What to expect |
|---|---|
| [Hosted recorded demo](https://receipts.nryn.dev/demo/) | Keyless, disposable simulation of the receipt and appeal flow. It is recorded fixture data, not a live Twitch session. |
| [Live rehearsal evidence](docs/evidence/t62-live-acceptance.md) | Real Twitch capture through an owner-approved stamp and chat verdict, with the synthetic promise and remaining OBS checks disclosed. |
| [Evaluation and model limits](docs/model-migration.md) | Model path, reviewed evaluation and why automatic stamps remain disabled. |
| [Run locally without keys](#run-without-keys) | Build the app, preview the overlay and run deterministic replay and tests. |
| [Connect your own channel](docs/submission.md#try-your-own-twitch-channel) | Twitch sign-in, owner control and OBS pairing, plus current hosting limits. |
| [Features, limitations and Codex evidence](docs/submission.md) | Claims tied to evidence and the principal Codex build session ID. |

Read the [Receipts documentation](https://nrynss.github.io/receipts/) for setup, architecture and verified evidence.

The Starlight documentation site lives in [`site/`](site/). Install its pinned dependencies with `cd site && npm ci`, then build and check it with `npm run build && npm run check:links`.

A separately accessible recording of the live rehearsal or Codex build has not yet been verified for this guide. The hosted recorded demo is interactive fixture replay; the live rehearsal evidence is a written record, not a video.

## Run without keys

Install Go 1.27.1 or later in the 1.27 series, Node.js 24 or later, npm and ffmpeg/ffprobe. From the repository root, install dependencies once, then build and run the offline gate:

```sh
go mod download
(cd web && npm ci)
./tools/check.sh
```

Dependency installation needs network access. The gate itself needs no keys, network or live stream. It builds `bin/receipts`; on a restricted filesystem, set `GOCACHE` to a writable directory.

```sh
RECEIPTS_MODE=fixture ./bin/receipts serve
```

Open [the local overlay sequence](http://localhost:8080/?fixture=sequence) to preview its recorded visual states. Stop the server with Ctrl+C. This is a visual fixture, not a live Twitch session. To persist recorded transcript input through the ledger, run the following separately with a database path that does not yet exist:

```sh
./bin/receipts replay --fixture testdata/replay/foundation.json --db /tmp/receipts-fresh-replay.db
```

The CLI replay covers transcript persistence, not the whole product loop. The offline gate includes the full-loop acceptance test. See [development instructions](docs/development.md) for details and [operations](docs/operations.md) for a self-hosted live deployment.

## Implementation tracks

| Track | Progress | Scope |
|---|---|---|
| P0 — Feasibility | 4/4 | Twitch, audio and OBS probes |
| P1 — Foundation | 4/4 | Settings, ledger, contracts and deterministic replay driver |
| TA: Accounts and onboarding | 4/4 | Hosted demo, onboarding, populated owner isolation, revocation and OBS display acceptance verified |
| P2 — Offline receipt loop | 4/4 | Capture, detection, appeals, lore and deterministic full-loop replay verified |
| P3 — Overlay/controls | 3/3 | Overlay, owner controls and actual local OBS/SSE recovery verified |
| P4 — Twitch integration | 4/4 | Authorized adapters, chat, local clip fallback and real stream-end capture exit verified |
| P5 — Intelligence | 4/4 | Evaluation and human label review complete; automatic stamps remain disabled by policy |
| PU: UI and copy | 4/4 | Brand, overlay/demo, streamer setup and bounded chat feedback verified |
| P6: Release | 1/4 | Deployment and recovery verified; live rehearsal and judge readiness remain |

[TA implementation phase and acceptance criteria](dev-diary/PHASE-A-accounts.md), with [account scope](docs/accounts-scope.md). TA.1 identity/sessions and TA.2 channel isolation are implemented, TA.3 joins the overlay work, and TA.4 has verified the deployed judge experience. The remaining full rehearsal is tracked under T6.2.

## Streamer experience

Sign in with Twitch and use the private control dock at `/control`. Pair a Browser Source in OBS through setup. Chat can use `!receipt`, `!lore`, `!uphold` and `!overturn`; the streamer and mods also have `!receipts off` and `!void`. Contradictions are reviewed privately and need owner approval before a stamp appears. See the [judge guide](docs/submission.md) for the verified live path and its limits.

## Licence

Apache-2.0. See [`LICENSE`](LICENSE).

## Overlay visual fixtures

After building, run the fixture server and open `http://localhost:8080/?fixture=sequence` to preview idle, stamp, evidence, appeal and verdict states. Individual `?fixture=stamp`, `evidence`, `appeal` and `verdict` views are also available. This previews original UI fixtures; it does not execute the full receipt/appeal pipeline or validate real Twitch/OBS playback.

# Receipts

**The stream remembers what you said.** Built with OpenAI Codex.

Chat types `!receipt` when a streamer promises something on stream. Receipts clips the moment and keeps the claim. When the streamer later goes back on it, a **RECEIPT** stamp lands on the overlay with the old clip, and chat votes on the appeal. `!lore` gives new viewers the channel's running promises and the ones that broke.

> **Status:** entry for the Codex × TwitchCon Hackathon, in development. P0 feasibility, P1 foundation and the offline receipt loop are complete. Account isolation, onboarding/OBS pairing and owner controls are implemented. Verified live-session lifecycle and Twitch adapter checks pass. Production text intelligence now uses GPT-6 Luna for extraction and Decisions sensitivity/judging; transcription and embeddings remain unchanged. Contradictions still route to private review and automatic stamps remain disabled; see the [model migration and evaluation limits](docs/model-migration.md) and [development diary](dev-diary/README.md).

Implementation phases, task dependencies and acceptance criteria are in the [development diary](dev-diary/README.md).
Build and run the current server using the [development instructions](docs/development.md).

## Implementation tracks

| Track | Progress | Scope |
|---|---|---|
| P0 — Feasibility | 4/4 | Twitch, audio and OBS probes |
| P1 — Foundation | 4/4 | Settings, ledger, contracts and deterministic replay driver |
| TA — Accounts and onboarding | 3/4 | Identity, isolation and OBS pairing verified; anonymous recorded demo implemented; hosted judge acceptance pending |
| P2 — Offline receipt loop | 4/4 | Capture, detection, appeals, lore and deterministic full-loop replay verified |
| P3 — Overlay/controls | 3/3 | Overlay, owner controls and actual local OBS/SSE recovery verified |
| P4 — Twitch integration | 4/4 | Authorized adapters, chat, local clip fallback and real stream-end capture exit verified |
| P5 — Intelligence | 4/4 | Evaluation and human label review complete; automatic stamps remain disabled by policy |
| PU: UI and copy | 3/4 | Brand, overlay/demo and streamer setup complete; chat feedback remains |
| P6: Release | 0/4 | Deployment, live proof and judge readiness |

[TA implementation phase and acceptance criteria](dev-diary/PHASE-A-accounts.md), with [account scope](docs/accounts-scope.md). TA.1 identity/sessions and TA.2 channel isolation are implemented, TA.3 joins the overlay work, and TA.4 verifies the deployed judge experience before submission preparation.

## Current commands

```
RECEIPTS_MODE=fixture ./bin/receipts serve
./bin/receipts replay --fixture testdata/replay/foundation.json --db /tmp/receipts-fresh.db
```

The replay database path must be new. The CLI replay persists transcript fixtures through the real ledger. The T2.4 offline acceptance test exercises the full product loop through real services. Hosted auth setup is documented in [the TA.1 handoff](docs/ta1-auth-handoff.md).

## Planned streamer experience

Twitch sign-in and OBS pairing for the owner’s channel are implemented. Owners reach the private control dock at `/control` from account setup. Local OBS pairing/recovery acceptance passed. The anonymous recorded demo is available at `/demo/`. Verified session lifecycle and audio/intelligence integration are implemented. Contradictions require owner approval before a stamp appears. Hosted judge acceptance remains open.

Add the overlay to OBS as a browser source. Chat commands: `!receipt`, `!lore`, `!uphold`, `!overturn`; the streamer and mods also have `!receipts off` and `!void`.

## Licence

Apache-2.0. See [`LICENSE`](LICENSE).

## Overlay visual fixtures

After building, run the fixture server and open `http://localhost:8080/?fixture=sequence` to preview idle, stamp, evidence, appeal and verdict states. Individual `?fixture=stamp`, `evidence`, `appeal` and `verdict` views are also available. This previews original UI fixtures; it does not execute the full receipt/appeal pipeline or validate real Twitch/OBS playback.

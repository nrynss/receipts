# Receipts

**The stream remembers what you said.** Built with OpenAI Codex.

Chat types `!receipt` when a streamer promises something on stream. Receipts clips the moment and keeps the claim. When the streamer later goes back on it, a **RECEIPT** stamp lands on the overlay with the old clip, and chat votes on the appeal. `!lore` gives new viewers the channel's running promises and the ones that broke.

> **Status:** entry for the Codex × TwitchCon Hackathon, in development. P0 feasibility and P1 foundation are complete. Twitch account/session support (TA.1), deterministic replay infrastructure, the ledger and API/event contracts are implemented. The product receipt loop, hosted onboarding and shared live channel runtimes remain planned; see [`product.md`](product.md).

Implementation phases, task dependencies and acceptance criteria are in the [development diary](dev-diary/README.md).
Build and run the current server using the [development instructions](docs/development.md).

## Implementation tracks

| Track | Progress | Scope |
|---|---|---|
| P0 — Feasibility | 4/4 | Twitch, audio and OBS probes |
| P1 — Foundation | 4/4 | Settings, ledger, contracts and deterministic replay driver |
| TA — Accounts and onboarding | 2/4 | Identity and isolation implemented; OBS pairing and judge demo pending |
| P2 — Offline receipt loop | 2/4 | Capture/detection implemented; appeal and lore pending |
| P3–P5 — Live experience | Planned | Overlay, Twitch and intelligence |
| P6 — Release | Planned | Deployment, live proof and judge readiness |

[TA implementation phase and acceptance criteria](dev-diary/PHASE-A-accounts.md), with [account scope](docs/accounts-scope.md). TA.1 identity/sessions and TA.2 channel isolation are implemented, TA.3 joins the overlay work, and TA.4 verifies the deployed judge experience before submission preparation.

## Current commands

```
RECEIPTS_MODE=fixture ./bin/receipts serve
./bin/receipts replay --fixture testdata/replay/foundation.json --db /tmp/receipts-fresh.db
```

The replay database path must be new. Replay currently persists transcript fixtures through the real ledger; full product replay is T2.4 acceptance. Hosted auth setup is documented in [the TA.1 handoff](docs/ta1-auth-handoff.md).

## Planned streamer experience

Hosted onboarding will offer Twitch sign-in and OBS pairing for the owner’s channel, plus a recorded demo without login. Identity/session and channel-isolation foundations are implemented; hosted onboarding, OBS pairing and live-adapter activation remain planned.

Add the overlay to OBS as a browser source. Chat commands: `!receipt`, `!lore`, `!uphold`, `!overturn`; the streamer and mods also have `!receipts off` and `!void`.

## Licence

Apache-2.0. See [`LICENSE`](LICENSE).

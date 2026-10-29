# Receipts

**The stream remembers what you said.** Built with OpenAI Codex.

Chat types `!receipt` when a streamer promises something on stream. Receipts clips the moment and keeps the claim. When the streamer later goes back on it, a **RECEIPT** stamp lands on the overlay with the old clip, and chat votes on the appeal. `!lore` gives new viewers the channel's running promises and the ones that broke.

> **Status:** entry for the Codex × TwitchCon Hackathon, in development. The server/overlay scaffold, offline gate and evaluation fixtures exist. The live receipt and appeal experience below is still planned; see [`product.md`](product.md).

Implementation phases, task dependencies and acceptance criteria are in the [development diary](dev-diary/README.md).
Build and run the current scaffold using the [development instructions](docs/development.md).

## Implementation tracks

| Track | Progress | Scope |
|---|---|---|
| P0 — Feasibility | 4/4 | Twitch, audio and OBS probes |
| P1 — Foundation | 3/4 | Settings, ledger, contracts; replay next |
| TA — Accounts and onboarding | 0/4 | Twitch login, channel isolation, OBS pairing and judge demo |
| P2–P5 — Product experience | Planned | Receipt loop, overlay, Twitch and intelligence |
| P6 — Release | Planned | Deployment, live proof and judge readiness |

[TA implementation phase and acceptance criteria](dev-diary/PHASE-A-accounts.md), with [account scope](docs/accounts-scope.md). TA.1 can start alongside T1.4; TA.2 enables shared channel runtimes, TA.3 joins the overlay work, and TA.4 verifies the deployed judge experience before submission preparation.

## Usage (planned)

```
receipts serve   # server, channel runtimes and web app
```

Hosted onboarding will offer Twitch sign-in and OBS pairing for the owner’s channel, plus a recorded demo without login. This account track is planned, not implemented.

Add the overlay to OBS as a browser source. Chat commands: `!receipt`, `!lore`, `!uphold`, `!overturn`; the streamer and mods also have `!receipts off` and `!void`.

## Licence

Apache-2.0. See [`LICENSE`](LICENSE).

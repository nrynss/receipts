# Receipts implementation diary

[Product](../product.md) defines behavior. [AGENTS.md](../AGENTS.md) defines implementation rules. These documents order the build; where they conflict with either, fix the plan before implementation. No dates or timeboxes: dependencies determine order.

Build one complete moment early: a promise is captured, a spoken contradiction produces an evidence-backed stamp, and Twitch chat votes on the appeal. Replay proves the product loop; a real Twitch rehearsal proves the integration.

## Phase board

| Phase | Document | Complete | Status |
|---|---|---|---|
| P0 | [Ground and feasibility](PHASE-0-ground.md) | 4 / 4 | Complete; owner confirmed OBS video, audio and autoplay |
| P1 | [Contracts, storage and lifecycle](PHASE-1-contracts.md) | 4 / 4 | Complete; replay driver landed; full product scenario acceptance belongs to T2.4 |
| TA | [Accounts and judge onboarding](PHASE-A-accounts.md) | 3 / 4 | Identity, isolation and onboarding/OBS pairing complete; hosted judge acceptance remains |
| P2 | [Offline receipt and appeal loop](PHASE-2-receipt-loop.md) | 4 / 4 | Complete; capture, detection, appeals, lore and full offline replay verified |
| P3 | [OBS overlay and streamer controls](PHASE-3-overlay.md) | 3 / 3 | Complete; recovery and owner-reported real OBS rehearsal passed |
| P4 | [Live Twitch chat and evidence](PHASE-4-twitch.md) | 4 / 4 | Complete; authorized adapters, chat, local clip fallback and real stream-end authority/capture exit verified |
| P5 | [Live transcription and contradiction judging](PHASE-5-intelligence.md) | 0 / 4 | Not started |
| P6 | [Live proof, operations and submission](PHASE-6-ship.md) | 0 / 4 | Not started |

## Dependency map

```text
P0 ground ──> P1 contracts ──> P2 offline receipt loop
                    │                  │
                    └──> P3 overlay <──┘
P0 Twitch probe + P1 + P2 ──> P4 live Twitch
P0 audio probe + P4 session authority ──> P5 live intelligence
P1 settings/store ──> TA.1 identity ──> TA.2 channel isolation
TA.2 + P3 overlay ──> TA.3 onboarding and OBS pairing
TA.2 ──> shared P4/P5 live runtimes
P3 + P4 + P5 + TA.3 ──> T6.1 deployment
T6.1 + replay/product loop + TA.3 ──> TA.4 hosted acceptance
T6.2 real rehearsal + TA.4 ──> T6.3 judge readiness
```

The map shows broad ordering. Each task's `requires` list is authoritative; phases need not close as whole blocks. T3.1 can start after T1.3 while the receipt loop is being built. Probes happen early so an API or capture limitation is found before dependent implementation. This is an ordering plan, not authorization to spawn agents; keep most product work in the principal Codex thread.

## Task protocol

Each task has a stable `T<phase>.<task>` ID (or `TA.<task>` for the accounts track), binding dependencies, fixture readiness, owned paths, status, implementation brief and observable done criteria. `fixture-ok: yes` means its implementation and offline checks can proceed with fixtures; any separately stated live acceptance remains required. Proposed paths describe ownership boundaries, not code that already exists.

Use `not-started`, `in-progress`, `blocked` or `done`. Name a blocker and the next concrete resolution. Update status only with evidence. Claim the task before editing, coordinate shared paths, and record any contract change plus its consumers in the handoff. No task is complete because its code merely compiles.

For a completed task record the revision, relevant checks, evidence location and limitations. Phase closure reconciles task status and the board. Do not create empty completion claims, invented metrics or future schedules.

## Scope boundaries

- Hosted scope: multiple Twitch owners, one owned channel per account, one Go process and one SQLite file, with isolated channel runtimes and overlays. See [accounts and judge onboarding scope](../docs/accounts-scope.md); TA.1 identity/sessions and TA.2 runtime isolation are implemented; TA.3 is complete with local OBS acceptance; TA.4 recorded demo is implemented; deployed acceptance remains open.
- Viewers use existing Twitch chat commands; the streamer uses a private control page/OBS dock.
- Explicit spoken promises and spoken contradictions first. Visual gameplay recognition and billing are deferred. Self-service Twitch onboarding is now required by the hosted scope.
- Capture preserves transcript evidence even if a clip is pending, late or unavailable. A transcript fallback is clearly labelled.
- Streamer attribution is required. Broadcast transcription alone does not establish who spoke; uncertain guests, quotations and media must abstain or enter review.
- A wrong stamp is worse than a missed one. Sensitive and voided claims stay out of the public overlay and lore.
- Mic fallback follows the product's per-live-session enable/stop rules. Pausing stamps and stopping listening are different controls.

## Verification and evidence

`./tools/check.sh` is the offline commit gate: formatting, Go vet/tests, overlay lint/type checks/tests. Checks use original or sanitized recorded fixtures under `testdata/`, no keys or live services. Installing dependencies is separate from running the offline gate.

Real-service tests use the `live` build tag. Every live run has an explicit spend cap: one case first, inspect usage, then continue only within the cap. Record actual service evidence separately from replay evidence. Verify current provider documentation when implementing adapters; product sketches do not pin current API schemas or available models.

Capture Codex build footage as work happens, with credentials hidden. Keep the principal build session ID for the hackathon submission. Preserve original content ownership and redact recorded service payloads. The event requirements in the product are planning inputs; recheck official rules at submission readiness.

## Critical demo path

T0.1 → T0.4 → T1.2 → T1.3 → T1.4 → T2.1 → T2.2 → T2.3 → T2.4 establishes the offline product. T3.1–T3.3 make it visible in OBS. T0.2/T0.3 prove the live assumptions; P4 and P5 replace recorded adapters. T6.2 proves the real Twitch experience; T6.3 packages that evidence for judges.

## Current handoff

| Landed work | Revision | Evidence / limitation |
|---|---|---|
| P0 feasibility | `c7682b0` | [P0 evidence](PHASE-0-ground.md); owner-reported local OBS acceptance; deployed origin still unverified |
| T1.1 settings and T1.2 ledger | `19795cf` | Explicit modes, durable transitions and startup migration; offline gate and race checks |
| T1.3 contracts | `96b4c43` | Go/TS goldens, API authorization and SSE recovery; hosted router/scoped reads mounted by TA.2; P2 lifecycle actions implemented |
| Keel upgrade | `142c2a3` | v0.9.0; [compatibility review](../docs/keel-upgrade.md) |
| T1.4 replay foundation | `ced2280` | Strict offline dispatch, injected clock/IDs and real ledger determinism; full product demo is T2.4 |
| TA.1 identity and sessions | `ed2c7d6` | OAuth, encrypted tokens, session/CSRF and validation lifecycle; [auth handoff](../docs/ta1-auth-handoff.md); real hosted consent remains TA.4 |
| T2.1 receipt capture | `4150ce5` | Exact attributed evidence, durable dedup/throttles and clip jobs; live extraction/transport remain downstream |
| TA.2 channel isolation | `3b9242c` | Runtime/API/device isolation and durable budgets; [runtime handoff](../docs/ta2-runtime-handoff.md); live activation remains P4 |
| T2.2 detection/review | `0190538` | Delayed-result guards, durable cooldown and replay/SSE recovery |
| T3.1 overlay states | `090acd8` | Reviewed original fixture UI and mounted browser checks; authenticated SSE and local actual OBS acceptance passed |

| T2.3 appeals | `75d2a52` | Guarded decisions, votes, review and restart recovery |
| TA.3 onboarding | `e1821fa` | Account lifecycle and OBS pairing implemented; local OBS refresh/restart/revocation acceptance passed |
| T4.1 authorization | `5d33818` | Bound adapter confirmed live nrynss, sent one approved message and created one clip; readiness remains T4.3 |
| T2.4 lore/replay | `418121b` | Two fresh stores, complete event payload comparison, failed clip and restart |
| T3.2 owner controls | `a796c8b` candidate | Reviewed dock, keyboard rehearsal, retry recovery; integrated with this handoff |

| T4.4 authority implementation | `17a2a74` | Generation/freshness/stop guards verified offline; real stream-end capture exit passed |
| T4.3 clip implementation | `0b6685a` candidate | Real SQLite leases/restart/retry checks and existing-clip live readiness; OBS/deployed playback acceptance remains |
| T4.2 transport | `48f0ce4` | Real Twitch five-command acceptance; [evidence](../docs/evidence/t42-live-transport.json) |
| TA.4 anonymous demo | `431ca1e` | Isolated recorded replay/reset and browser checks; deployment/fresh two-owner acceptance remains |
| T3.3 / TA.3 acceptance | `3e4b9f1` + principal evidence | [Real OBS owner reports and persisted restart checks](../docs/evidence/t33-ta3-obs.json); fresh hosted onboarding remains TA.4 |

The integrated capture/isolation tree passed the full offline gate and uncached auth/API/events/runtime/settings/store/commands/startup race checks. Provider HTTP tests use an in-memory transport, so the gate requires no localhost socket permissions. The full offline receipt loop is verified; deployed multi-streamer acceptance remains open.

## Ready next

T3.3 recovery and actual local OBS rehearsal are complete. T4.2 EventSub/chat transport is complete. T4.3 clip jobs and local OBS fallback acceptance are complete; video/deployed playback and actual feedback checks remain release acceptance. TA.4 anonymous recorded demo is implemented; full hosted acceptance waits for T6.1. T4.4 live-session authority and real capture shutdown acceptance are complete. T5.1 audio lifecycle/transcription is ready; production audio/transcription and chat/clip composition remain downstream integration. TA.3 local OBS acceptance and T4.1 bounded live capability acceptance passed. T6.1 deploys after TA.3; TA.4 verifies the deployed experience and gates T6.3 judge readiness.

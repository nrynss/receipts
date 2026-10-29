# Receipts implementation diary

[Product](../product.md) defines behavior. [AGENTS.md](../AGENTS.md) defines implementation rules. These documents order the build; where they conflict with either, fix the plan before implementation. No dates or timeboxes: dependencies determine order.

Build one complete moment early: a promise is captured, a spoken contradiction produces an evidence-backed stamp, and Twitch chat votes on the appeal. Replay proves the product loop; a real Twitch rehearsal proves the integration.

## Phase board

| Phase | Document | Complete | Status |
|---|---|---|---|
| P0 | [Ground and feasibility](PHASE-0-ground.md) | 4 / 4 | Complete; owner confirmed OBS video, audio and autoplay |
| P1 | [Contracts, storage and lifecycle](PHASE-1-contracts.md) | 4 / 4 | Complete; replay driver landed; full product scenario acceptance belongs to T2.4 |
| TA | [Accounts and judge onboarding](PHASE-A-accounts.md) | 1 / 4 | Identity/sessions reviewed; TA.2 channel authorization and isolation next |
| P2 | [Offline receipt and appeal loop](PHASE-2-receipt-loop.md) | 0 / 4 | Not started |
| P3 | [OBS overlay and streamer controls](PHASE-3-overlay.md) | 0 / 3 | Not started |
| P4 | [Live Twitch chat and evidence](PHASE-4-twitch.md) | 0 / 4 | Not started |
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

- Hosted scope: multiple Twitch owners, one owned channel per account, one Go process and one SQLite file, with isolated channel runtimes and overlays. See [accounts and judge onboarding scope](../docs/accounts-scope.md); TA.1 identity/sessions is implemented; TA.2–TA.4 remain planned.
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
| T1.3 contracts | `96b4c43` | Go/TS goldens, API authorization and SSE recovery; product services/router mounting still pending |
| Keel upgrade | `142c2a3` | v0.9.0; [compatibility review](../docs/keel-upgrade.md) |
| T1.4 replay foundation | `ced2280` | Strict offline dispatch, injected clock/IDs and real ledger determinism; full product demo is T2.4 |
| TA.1 identity and sessions | `ed2c7d6` | OAuth, encrypted tokens, session/CSRF and validation lifecycle; [auth handoff](../docs/ta1-auth-handoff.md); real hosted consent remains TA.4 |

The combined tree at `ed2c7d6` passed the full offline gate and uncached auth/replay/store/settings/API/events/startup race checks. No deployed multi-streamer or full receipt-loop acceptance is claimed.

## Ready next

T2.1 receipt capture, TA.2 channel authorization/runtime isolation and T3.1 overlay states have completed prerequisites. No implementation agent is active. Later tasks retain their explicit dependencies. T6.1 deploys after TA.3; TA.4 verifies that deployment and gates T6.3 judge readiness.

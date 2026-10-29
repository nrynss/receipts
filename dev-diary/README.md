# Receipts implementation diary

[Product](../product.md) defines behavior. [AGENTS.md](../AGENTS.md) defines implementation rules. These documents order the build; where they conflict with either, fix the plan before implementation. No dates or timeboxes: dependencies determine order.

Build one complete moment early: a promise is captured, a spoken contradiction produces an evidence-backed stamp, and Twitch chat votes on the appeal. Replay proves the product loop; a real Twitch rehearsal proves the integration.

## Phase board

| Phase | Document | Complete | Status |
|---|---|---|---|
| P0 | [Ground and feasibility](PHASE-0-ground.md) | 4 / 4 | Complete; owner confirmed OBS video, audio and autoplay |
| P1 | [Contracts, storage and lifecycle](PHASE-1-contracts.md) | 3 / 4 | Settings, ledger and API/event contracts complete; deterministic replay next |
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
P3 + P4 + P5 evaluation ──> P6 deploy, real rehearsal, submission
```

The map shows broad ordering. Each task's `requires` list is authoritative; phases need not close as whole blocks. P3.1 can start after T1.3 while the receipt loop is being built. Probes happen early so an API or capture limitation is found before dependent implementation. This is an ordering plan, not authorization to spawn agents; keep most product work in the principal Codex thread.

## Task protocol

Each task has a stable `T<phase>.<task>` ID, binding dependencies, fixture readiness, owned paths, status, implementation brief and observable done criteria. `fixture-ok: yes` means its implementation and offline checks can proceed with fixtures; any separately stated live acceptance remains required. Proposed paths describe ownership boundaries, not code that already exists.

Use `not-started`, `in-progress`, `blocked` or `done`. Name a blocker and the next concrete resolution. Update status only with evidence. Claim the task before editing, coordinate shared paths, and record any contract change plus its consumers in the handoff. No task is complete because its code merely compiles.

For a completed task record the revision, relevant checks, evidence location and limitations. Phase closure reconciles task status and the board. Do not create empty completion claims, invented metrics or future schedules.

## Scope boundaries

- One authorized Twitch channel, one Go process, one SQLite file and one OBS overlay.
- Viewers use existing Twitch chat commands; the streamer uses a private control page/OBS dock.
- Explicit spoken promises and spoken contradictions first. Visual gameplay recognition, public multi-channel onboarding and billing are deferred.
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

## Handoff log

P0 implementation began in the principal Codex build. T0.4 has an original offline evaluation contract and 44-pair corpus. T0.1 is complete: pinned dependencies install, the full offline gate passes, and the Go binary serves the generated Svelte overlay. Development instructions are in `docs/development.md`. T0.2 and T0.3 have executable bounded live probes and passing offline tests. Twitch registration and authorization are verified. Live chat receive/reply, clip readiness, local iframe playback and short broadcast capture have passed. The active audio lifecycle test now passes as well; owner-reported OBS playback at localhost now passes; production-origin checks remain P4/P6 work.

Live follow-up: chat receive/reply, clip readiness and short broadcast capture pass on nrynss. Local clip playback reaches the end. See P0 handoff for measured evidence and the remaining OBS playback check.

Active lifecycle follow-up: T0.3 is complete after controlled source interruption, fresh reconnect with audio, real stream-end cleanup and authoritative offline confirmation. Evidence preserves source EOF versus later status-confirmation timing. The owner subsequently confirmed actual OBS playback and autoplay, closing T0.2 and P0.

P0 closed: all four tasks complete. OBS video, audio, autoplay and click-to-play were confirmed by the owner at localhost. P1 contracts/storage is the next phase.

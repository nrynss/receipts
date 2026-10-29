# P1: Contracts, storage and lifecycle

**Goal:** Settle the shared data shapes and state transitions so the same pipeline accepts replay and live inputs.

**Status:** in-progress

Read [the phase index](README.md), [product spec](../product.md) and [agent protocol](../AGENTS.md) first. Task-level dependencies below are binding.

### T1.1: Settings and startup

```yaml
requires: T0.1
fixture-ok: yes
owns: internal/settings/, cmd/receipts/, .env.example
status: in-progress
```

Use Keel config for process settings and environment-sourced secrets. Separate fixture and live startup explicitly. Settings include public origin, channel identity, transcript retention, stamp cooldown, appeal duration, live-status freshness, model IDs and spend ceilings. Keep credentials out of logs and URLs. Inspect supported Keel APIs rather than inventing configuration syntax.

**Done when:** Fixture startup succeeds without secrets. Live startup names missing settings without printing values. Invalid limits or origins are rejected.

### T1.2: Ledger and transitions

```yaml
requires: T0.4
fixture-ok: yes
owns: internal/store/, internal/receipt/, testdata/store/
status: in-progress
```

Use Keel SQLite with versioned migrations. Define channel, live session, timestamped transcript segment, receipt, evidence, detection, review, appeal, vote and verdict records. Keep receipt validity separate from clip readiness and verdict. Evidence retains exact source text, segment IDs, session-relative offsets and attribution. Define expiry/retention and void filtering; embeddings and lore must respect voids. Use bounded transcript storage. Transitions and vote uniqueness must be transactional.

**Done when:** Migrations are repeatable. Restart preserves receipts and verdicts. Duplicate detections, repeated decisions and concurrent votes cannot create duplicate stamps or verdicts. Voided records disappear from retrieval and lore.

### T1.3: API, event and adapter contracts

```yaml
requires: T1.1, T1.2
fixture-ok: yes
owns: internal/api/, internal/events/, internal/ports/, web/src/lib/api/, testdata/contracts/
status: not-started
```

Define typed inputs for transcript segments, chat commands, clip results and live-session changes; adapters do not own product decisions. Specify the read-only overlay snapshot/event stream, authenticated streamer controls, review queue and health endpoints. Define ordered event IDs, reconnect snapshots and bounded replay. Mirror Go payloads in TypeScript with golden examples. Overlay credentials grant read access only; control credentials never enter the browser-source URL.

**Done when:** Golden payloads decode on both sides. Reconnect restores current state without replaying an old stamp as new. Authorization tests cover permitted reads and refused control writes.

### T1.4: Deterministic replay driver

```yaml
requires: T1.3, T0.4
fixture-ok: yes
owns: internal/replay/, testdata/replay/, cmd/receipts/
status: not-started
```

Build an explicitly selected fixture mode with a controllable clock and recorded adapter responses. Drive the real application services rather than a second demo implementation. Fix input order and stable IDs; no credential fallback can cause fixture mode to call Twitch or OpenAI.

**Done when:** The original demo can be replayed twice into separate fresh stores with the same event and verdict sequence, with no external calls.

## Exit criteria

- [ ] Every task above has evidence against its done criteria.
- [ ] The offline gate passes on the candidate revision.
- [ ] Live checks are separately identified, budgeted and never part of the offline gate.
- [ ] Outstanding limitations and downstream contract changes are recorded.

## Handoff log

### What exists now

T1.1 settings/startup and T1.2 ledger/transitions are assigned to separate Luna agents in isolated worktrees. No implementation or verification is claimed yet. The principal thread reviews and integrates each result; agents do not share task-owned files.

### Evidence and decisions

Record task IDs, relevant revision, checks and artifact paths when work lands. Never record secrets.

### Notes for the next task

Record unresolved blockers, contract changes and the next unblocked task.

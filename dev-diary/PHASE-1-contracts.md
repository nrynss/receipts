# P1: Contracts, storage and lifecycle

**Goal:** Settle the shared data shapes and state transitions so the same pipeline accepts replay and live inputs.

**Status:** in-progress

Read [the phase index](README.md), [product spec](../product.md) and [agent protocol](../AGENTS.md) first. Task-level dependencies below are binding.

### T1.1: Settings and startup

```yaml
requires: T0.1
fixture-ok: yes
owns: internal/settings/, cmd/receipts/, .env.example
status: done
```

Use Keel config for process settings and environment-sourced secrets. Separate fixture and live startup explicitly. Settings include public origin, channel identity, transcript retention, stamp cooldown, appeal duration, live-status freshness, model IDs and spend ceilings. Keep credentials out of logs and URLs. Inspect supported Keel APIs rather than inventing configuration syntax.

**Done when:** Fixture startup succeeds without secrets. Live startup names missing settings without printing values. Invalid limits or origins are rejected.

### T1.2: Ledger and transitions

```yaml
requires: T0.4
fixture-ok: yes
owns: internal/store/, internal/receipt/, testdata/store/
status: done
```

Use Keel SQLite with versioned migrations. Define channel, live session, timestamped transcript segment, receipt, evidence, detection, review, appeal, vote and verdict records. Keep receipt validity separate from clip readiness and verdict. Evidence retains exact source text, segment IDs, session-relative offsets and attribution. Define expiry/retention and void filtering; embeddings and lore must respect voids. Use bounded transcript storage. Transitions and vote uniqueness must be transactional.

**Done when:** Migrations are repeatable. Restart preserves receipts and verdicts. Duplicate detections, repeated decisions and concurrent votes cannot create duplicate stamps or verdicts. Voided records disappear from retrieval and lore.

### T1.3: API, event and adapter contracts

```yaml
requires: T1.1, T1.2
fixture-ok: yes
owns: internal/api/, internal/events/, internal/ports/, web/src/lib/api/, testdata/contracts/
status: done
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

T1.1 settings/startup is reviewed and landed through `424b865` after independent full-gate and race verification. It uses Keel config, explicit fixture/live modes, validated settings and redacted secret references. T1.2 ledger/transitions is reviewed from `3b26ac0` and integrated with startup on the same caller-owned Keel SQLite handle. The principal thread owns shared documentation and startup integration.

### Evidence and decisions

T1.1 landed as `2b88551` plus review fixes `424b865`. T1.2 candidate `3b26ac0` passed independent full-gate and ledger race checks before integration. The combined tree also initializes/migrates the ledger during configured startup and checks this in the fixture startup test. No live calls or credentials are needed for verification.

### Notes for the next task

T1.3 is reviewed and landed through `6e1e75b`; T1.4 deterministic replay is next. Reuse `internal/settings`, `internal/receipt` and `store.New(ctx, db, retention, now)`; the application owns the shared DB lifetime. Detection source session IDs refer to the later live broadcast, independently of the original receipt session. Store evidence snapshots survive transcript pruning. `PublicReceipts` is the filtered surface for matching/lore; raw `GetReceipt` is for authorized internal/admin use. P1 is 3/4 complete and remains in progress until T1.4 deterministic replay is complete.

### Review decisions

The settings review fixed prefixed environment lookup, finite spend validation, malformed origin ports and formatting/JSON redaction. Live model IDs are explicit configuration, not assumed defaults. The ledger review fixed exact source/channel ownership, later-session contradictions, review eligibility after void/expiry, durable idempotency keys, tie/no-vote closure and bounded transcript retention. Existing decision-key retries return the original record without rewriting a newer verdict. Transcript rows are globally capped and age-pruned on writes; aged reads are filtered. Evidence is retained separately as exact snapshots.

The current startup serves the scaffold and initializes storage; it does not start Twitch listening or OpenAI calls. T1.3 provides the authenticated router, typed adapter ports, bounded event publication and TypeScript reconnect client. Application service implementations and mounting the router remain integration work; unconnected control actions return 501. Deterministic replay will need stable ID handling in T1.4; storage currently uses Keel-generated IDs and an injectable clock.

T1.3 passed the principal thread’s full offline gate and uncached API/event race tests at `6e1e75b`. Go and TypeScript golden payloads cover adapter inputs, overlay state, review, health and control requests. Review fixes serialize snapshot/publication order, reset transport cursors after restart, discard old-epoch buffered frames and ignore late snapshot responses from superseded connections. Read and control credentials are separate and fail closed when equal. No live calls or new dependencies were required.

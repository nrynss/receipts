# P5: Live transcription and contradiction judging

**Goal:** Replace recorded model/audio adapters with bounded OpenAI calls and measure the behavior.

**Status:** in-progress (0/4)

Read [the phase index](README.md), [product spec](../product.md) and [agent protocol](../AGENTS.md) first. Task-level dependencies below are binding.

### T5.1: Audio lifecycle and transcription

```yaml
requires: T0.3, T4.4, T1.1, TA.2
fixture-ok: yes
owns: internal/audio/, internal/transcribe/, testdata/audio/, testdata/models/
status: in-progress
```

Use the selected source and current official OpenAI speech documentation. Choose and record a supported model and audio format. Timestamp chunks on the session clock, bound buffering, deduplicate overlaps and define partial/final segment handling. Only final attributed speech can support claims or stamps. With mixed broadcast audio, use an explicit supported attribution strategy or abstain; transcription alone does not identify the streamer. Stop and discard unsent buffers when the session stops.

**Done when:** Offline audio/response fixtures prove ordering and overlap handling. A budgeted live case produces correctly aligned evidence and stops cleanly on offline or source failure.

### T5.2: Claim extraction and retrieval

```yaml
requires: T5.1, T2.1
fixture-ok: yes
owns: internal/claims/, internal/retrieval/, testdata/models/
status: not-started
```

Choose supported OpenAI extraction/judge and embedding models from current official documentation; pin configurable IDs. Extract a normalized claim with exact evidence references and topic/attribution checks. Embed active eligible receipts and retrieve the nearest 3–5 with plain cosine in Go. Store embedding model/version and handle dimension changes through explicit reindexing. Transcript and chat are untrusted data, never model instructions.

**Done when:** Recorded malformed output, quotation, prompt injection, sensitive text and ambiguous speakers cannot create an unsupported public claim. Retrieval tests use fixed vectors with known nearest results.

### T5.3: Judge, budgets and failures

```yaml
requires: T5.2, T2.2, TA.2
fixture-ok: yes
owns: internal/judge/, internal/budget/, testdata/models/
status: not-started
```

Judge retrieved claim/later-line pairs with structured contradicts, does-not-contradict or unsure plus evidence references. Require validated evidence and calibrated policy before stamping; a model confidence number alone is insufficient. Use Keel v0.9.0 gate/cost and durable context-aware metering for bounded concurrency, per-channel/session and installation-wide spend, with an explicit preflight live budget. Reserve before calls and reconcile actual usage. Bound retries/timeouts and distinguish unavailable service from unsure. Failed or late results never fabricate a stamp.

**Done when:** Fixtures cover refusal, invalid JSON, rate limits, timeout, exhausted budget and concurrent cap contention. One live case runs first; usage is checked before any larger run.

### T5.4: Evaluation and threshold decision

```yaml
requires: T5.3, T0.4
fixture-ok: yes
owns: internal/eval/, testdata/eval/, docs/evaluation.md
status: not-started
```

Run the labelled corpus through the same retrieval/judge policy used by the app. Tune only on the tuning partition, freeze the policy, then report held-out counts, denominators, false stamps, precision, recall and abstentions. Also report retrieval misses, latency and measured spend. Version prompt, model, policy and dataset identifiers. Sensitive or unattributed cases must never auto-stamp. Any false stamp requires inspection and a documented decision before enabling automatic live stamps.

**Done when:** The offline report reproduces from recorded replies and identifies its provenance. A separately labelled budgeted live-model report records actual results without presenting fixture scores as live-model accuracy.

## Exit criteria

- [ ] Every task above has evidence against its done criteria.
- [ ] The offline gate passes on the candidate revision.
- [ ] Live checks are separately identified, budgeted and never part of the offline gate.
- [ ] Outstanding limitations and downstream contract changes are recorded.

## Handoff log

### What exists now

P5 model/transcription integration has not started. P0 audio feasibility and the v0.9.0 Keel compatibility review exist; neither proves model accuracy or long-running hosted transcription.

### Evidence and decisions

Record task IDs, relevant revision, checks and artifact paths when work lands. Never record secrets.

### Notes for the next task

Record unresolved blockers, contract changes and the next unblocked task.

T5.1 is assigned to a Terra worktree from `b0db9d2`. The principal thread tracks source timing, attribution, lifecycle cancellation, durable budget settlement and production composition through review. Recorded/offline verification is separate from the required bounded paid-provider acceptance; no live transcription result is claimed during implementation.

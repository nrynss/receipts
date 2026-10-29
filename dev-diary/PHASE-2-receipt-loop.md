# P2: Offline receipt and appeal loop

**Goal:** Complete the promise-to-stamp-to-verdict experience using recorded inputs before wiring live services.

**Status:** not-started

Read [the phase index](README.md), [product spec](../product.md) and [agent protocol](../AGENTS.md) first. Task-level dependencies below are binding.

### T2.1: Take a receipt

```yaml
requires: T1.4
fixture-ok: yes
owns: internal/commands/, internal/receipt/, testdata/replay/
status: not-started
```

Handle !receipt with channel-scoped deduplication, per-user/channel throttling and a bounded lookback. Extract only from attributed transcript segments; chat is a trigger, never claim evidence. Suppress sensitive claims before saving a public receipt or replying with its text. Record who captured it and original evidence. Create clip work separately so pending or failed clips do not lose the text receipt. Explain no eligible claim and unavailable transcript without fabricating evidence.

**Done when:** A fixture command creates one credited receipt with exact text/offsets. Duplicate commands do not multiply work. Missing speech, guest speech and sensitive claims produce no public claim.

### T2.2: Detect and queue

```yaml
requires: T2.1
fixture-ok: yes
owns: internal/detect/, testdata/models/, testdata/replay/
status: not-started
```

Use a judge port backed initially by recorded responses. Separate contradicts, does-not-contradict and unsure from the application decision to stamp. Apply attribution, sensitive-topic, void, cooldown and duplicate checks before emission. Uncertain eligible detections enter an authenticated quiet review queue. Recheck current state before a delayed result is published.

**Done when:** The scripted contradiction emits one stamp. A near miss emits none. Pause, void, a stale session or a delayed duplicate prevents publication even if a recorded judge response says contradicts.

### T2.3: Answer and appeal

```yaml
requires: T2.2
fixture-ok: yes
owns: internal/appeal/, internal/commands/, internal/api/
status: not-started
```

Implement Admit, Appeal and Context changed as authenticated streamer actions. Define !void for streamer/moderators and !receipts off/on with verified roles. Distinguish stamp pause from stopping audio. Count one vote per Twitch user per active appeal; accept !uphold and !overturn only in the correct channel and voting window. First vote is final for this version. A tie or no votes produces an explicit inconclusive verdict. Persist deadlines and close overdue appeals on recovery without replaying the stamp.

**Done when:** Concurrent/repeated clicks yield one result. Unauthorized controls fail. Boundary-time votes, duplicate deliveries, ties, no votes and restart have deterministic outcomes.

### T2.4: Lore and replay acceptance

```yaml
requires: T2.3
fixture-ok: yes
owns: internal/lore/, internal/replay/, testdata/replay/
status: not-started
```

Render !lore from stored claims and verdicts with a bounded response and a defined score calculation. Filter sensitive and voided content on every read. Exercise the original full scenario through capture, contradiction, appeal and lore, including a failed clip and restart.

**Done when:** One offline acceptance test verifies the full persisted event sequence and lore. Every displayed score reconciles with the ledger and suppressed claims never leak.

## Exit criteria

- [ ] Every task above has evidence against its done criteria.
- [ ] The offline gate passes on the candidate revision.
- [ ] Live checks are separately identified, budgeted and never part of the offline gate.
- [ ] Outstanding limitations and downstream contract changes are recorded.

## Handoff log

### What exists now

Planning only. No implementation is claimed.

### Evidence and decisions

Record task IDs, relevant revision, checks and artifact paths when work lands. Never record secrets.

### Notes for the next task

Record unresolved blockers, contract changes and the next unblocked task.

# P6: Live proof, operations and submission

**Goal:** Deliver a reproducible Twitch experience and evidence that it was built with Codex.

**Status:** not-started

Read [the phase index](README.md), [product spec](../product.md) and [agent protocol](../AGENTS.md) first. Task-level dependencies below are binding.

### T6.1: Deploy and recover

```yaml
requires: T3.3, T4.3, T5.4, TA.3
fixture-ok: yes
owns: deploy/, docs/operations.md, cmd/receipts/
status: not-started
```

Package one binary with the overlay and document ffmpeg/streamlink prerequisites. Deploy to the selected host with HTTPS, persistent SQLite, private controls and read-only overlay access. Check current host configuration before changing shared services. Bound disk/transcript retention, test backup restore and token revocation, and expose health without credentials or claim content. Configure the real embed origin.

**Done when:** A clean setup can follow the docs. Restart retains receipts and closes expired appeals once; restore recovers the ledger. Public access cannot reach control endpoints or private transcripts.

### T6.2: Real Twitch rehearsal

```yaml
requires: T6.1, T4.2
fixture-ok: no
owns: docs/evidence/, docs/obs.md
status: not-started
```

Run the original script on our channel: spoken promise, viewer !receipt, real clip, unrelated speech, spoken contradiction, stamp, streamer appeal, chat votes and !lore. Include pause/void, offline stop and one graceful failure. Record capture-to-receipt and contradiction-to-stamp latency, spend and any manual intervention. Keep fixture replay clearly labelled and separate from the real run.

**Done when:** Evidence demonstrates the entire real Twitch path, actual viewer-visible overlay and correct verdict. Any manual speaker restriction or transcript fallback is disclosed.

### T6.3: Judge-facing documentation and Codex evidence

```yaml
requires: T6.2, TA.4
fixture-ok: yes
owns: README.md, docs/evidence/, docs/submission.md
status: not-started
```

Build the For judges: start here table with working demo, recorded session, evaluation and run instructions. Record the principal Codex build session ID and describe concrete implementation work from this thread. Preserve build footage throughout implementation so this task assembles existing evidence. Write feature/limitation notes and a video script under three minutes showing real Twitch interaction and Codex building it. Use owned assets, English and no credentials in footage.

**Done when:** A new reader can run fixture mode without keys and find live setup instructions. Evidence links resolve, the session ID is verified, and the video demonstrates the claimed product behavior.

### T6.4: Submission readiness

```yaml
requires: T6.3
fixture-ok: no
owns: docs/submission.md
status: not-started
```

Recheck current official event rules and submission fields, registration, repository judge access, video visibility and Codex session-ID requirements. Compare every claim to evidence and run the offline gate on the exact candidate revision. Prepare materials for user review; uploading a draft is distinct from submitting. Record actual submission confirmation only after the user authorizes submission and the service confirms it.

**Done when:** The readiness checklist names the candidate revision and accessible artifacts, with no unresolved required field. No submission or award claim is made without confirmation.

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

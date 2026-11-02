# P6: Live proof, operations and submission

**Goal:** Deliver a reproducible Twitch experience and evidence that it was built with Codex.

**Status:** in-progress (1/4)

Read [the phase index](README.md), [product spec](../product.md) and [agent protocol](../AGENTS.md) first. Task-level dependencies below are binding.

### T6.1: Deploy and recover

```yaml
requires: T3.3, T4.3, T5.4, TA.3, TU.4
fixture-ok: yes
owns: deploy/, docs/operations.md, cmd/receipts/
status: done
```

Package one binary with the overlay and document ffmpeg/streamlink prerequisites. Deploy to the selected host with HTTPS, persistent SQLite, private controls and read-only overlay access. Check current host configuration before changing shared services. Bound disk/transcript retention, test backup restore and token revocation, and expose health without credentials or claim content. Configure the real embed origin and verify the Twitch clip embed or its truthful transcript fallback in OBS on that deployed origin.

**Done when:** A clean setup can follow the docs. Restart retains receipts and closes expired appeals once; restore recovers the ledger. Public access cannot reach control endpoints or private transcripts.

### T6.2: Real Twitch rehearsal

```yaml
requires: T6.1, T4.2
fixture-ok: no
owns: docs/evidence/, docs/obs.md
status: in-progress
```

Run the original script on our channel: spoken promise, viewer !receipt, real clip, unrelated speech, spoken contradiction, stamp, streamer appeal, chat votes and !lore. Include pause/void, offline stop and one graceful failure. Verify overlay clip audio cannot feed back into the real listening source; this check requires the T5.1 capture pipeline. Record capture-to-receipt and contradiction-to-stamp latency, spend and any manual intervention. Keep fixture replay clearly labelled and separate from the real run.

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

T6.1 deployment preparation is implemented and independently verified: `deploy/Dockerfile` packages the Go server and
overlay with ffmpeg and streamlink, `deploy/docker-run.sh` starts one
isolated Traefik tenant with the existing plain Docker pattern, and `deploy/backup.sh` / `deploy/restore.sh` manage
bounded backups and restore. `docs/operations.md` documents setup, recovery,
token revocation, disk limits and hosted OBS acceptance. Public `/healthz`
exposes only process liveness. Deployment and DNS are configured. Owner OAuth sign-in succeeded. No paid
model calls, stream capture, live token revocation or hosted OBS acceptance
has been performed during deployment checks.

### Evidence and decisions

Record task IDs, relevant revision, checks and artifact paths when work lands. Never record secrets. Host inventory is confirmed by read-only SSH: nine containers were running, the `proxy` network exists, and the root filesystem had 29 GiB free at inspection. This is a shared host; existing tenants include Traefik, Postgres, serp-relay, beforeyes, reprise, thutapi, mosaic, cerebros and FoleyFlow. The approved proxied `receipts.nryn.dev` DNS record is configured. The host has Python 3. The Compose plugin installed during preparation is unused; Receipts uses plain Docker like the existing tenants. Independent full gate and local SQLite recovery, traversal rejection, permission and unhealthy-start rollback fixtures passed. The hosted service is healthy over HTTPS, origin TLS is valid, private unauthenticated endpoints return 401, and owner OAuth sign-in succeeded. Owner-confirmed hosted OBS recorded fallback and actual hosted backup/restore acceptance passed. Both owner accounts and the current device-revocation state survived restore. T6.1 is complete; real Twitch clip/audio feedback remains T6.2. See [hosted deployment evidence](../docs/evidence/t61-hosted-deployment.md).

### Notes for the next task

Record unresolved blockers, contract changes and the next unblocked task.

The first hosted T6.2 rehearsal stopped at a no-eligible-claim result. Audio
and transcription worked; no receipt or clip was created. Final durable
accounting was $0.2389302 under the approved $0.25 cap, with zero unresolved
attempts. Further paid work is locked pending offline diagnosis and renewed
approval. See [partial pilot evidence](../docs/evidence/t62-hosted-pilot.md).

The subsequent staged run verified the core real path through owner approval,
OBS verdict, one chat vote, upheld appeal, lore, pause/resume and durable void.
Issue 2 is fixed and live-verified. Browser-poll cancellation and production
chat filtering were also repaired; empty-snapshot cleanup was corrected. TA.4
is complete. T6.2 still needs explicit actual clip playback/fallback and the
issue 1 restart observation; a visible verdict alone does not prove those.
Final settled accounting is $0.590510000, with zero allowance and pending
calls. See [current live evidence](../docs/evidence/t62-live-acceptance.md).

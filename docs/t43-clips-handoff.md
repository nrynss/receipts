# T4.3 clip readiness handoff

## Implemented

- Durable clip jobs now track create intent, retry deadlines, readiness polls and leases. A worker restart with an unresolved Twitch create intent marks the job failed as uncertain and does not issue a duplicate create request.
- Worker outcomes are fenced by the authenticated account, channel, live session, runtime generation, receipt eligibility and lease. Late results cannot publish after a void or session rollover.
- Create retries are bounded across restarts. `attempts` counts persisted create attempts, including explicit Twitch 429 rejections; readiness polls use the separate `polls` counter.
- Readiness lookup accepts the supported public Twitch clip URL forms, verifies the clip belongs to the authorized broadcaster, and uses the read-only `GET /clips` endpoint.
- A clip that becomes ready later is a video reference only. Transcript evidence remains authoritative; the delayed clip does not assert that it contains words the capture missed.

## Verification

- Real SQLite integration tests run capture through `commands.Service` and cover create, persisted ID, database reopen and readiness completion; simultaneous workers; crash recovery after create intent; stale lease, foreign owner, void and rollover fencing; and bounded 429 retries across restart.
- `GOCACHE=/tmp/receipts-go-cache ./tools/check.sh` passes.
- `GOCACHE=/tmp/receipts-go-cache go test -race -count=1 ./internal/twitch/clips ./internal/store ./internal/commands` passes.
- Read-only live readiness evidence for the existing authorized test clip is recorded in `docs/evidence/t43-live-readiness.json`. No clip was created during this work.

## Acceptance still open

- Playback from a deployed origin, OBS scene routing and end-to-end audio routing remain unverified. No deployed-origin proof is claimed. T4.4 activation remains disabled pending its own verification.
- `attempts` is create-attempt count; `polls` is readiness-poll count. Consumers should not treat the former as a combined request count.

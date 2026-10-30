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

## Local OBS playback acceptance

- The current production `/obs` page uses the paired-device routes and the overlay displays a saved claim alongside any clip iframe. A clip embed is click-to-play (`autoplay=false`); its status says only that the frame loaded and asks the OBS operator to verify playback, since cross-origin state cannot prove video or audio.
- Pending, unavailable, invalid URL, iframe error and iframe timeout keep the exact saved claim visible. The copy says the clip may omit words and transcript evidence remains authoritative.
- `web/src/lib/overlay/testing/overlay-checks.ts` covers fallback text, iframe error/timeout, retained claim, click-to-play configuration and the distinction between iframe load and playback.
- Historical owner-confirmed OBS video/audio/autoplay checks for the earlier P0 probe are recorded in `docs/evidence/p0-twitch.json`. Those do not prove this current overlay path. Record the current production-overlay OBS rehearsal separately in `docs/evidence/t43-obs.json` after the owner checks the current synthetic source.

## Remaining acceptance

- Current `/obs` playback, routing to the intended OBS scene, and audio behavior require owner confirmation in actual OBS. Do not record a successful playback/audio result from an iframe load or a browser-only check.
- Deployed-origin embed behavior remains T6.1/TA.4. Production feedback with capture remains T6.2 after T5.1. Do not infer transcript completeness from a clip that becomes ready later.
- `attempts` is create-attempt count; `polls` is readiness-poll count. Consumers should not treat the former as a combined request count.

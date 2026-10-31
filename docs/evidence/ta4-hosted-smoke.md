# TA.4 hosted smoke evidence

This is an anonymous, provider-zero smoke check of the deployed candidate at
`https://receipts.nryn.dev`. It does not establish T6.1 completion or full TA.4
acceptance.

## Hosted anonymous checks

- Public `/healthz` returned 200.
- Unauthenticated `/api/health` and `/api/snapshot` returned 401.
- `/demo/` returned 200 and the page identified the recorded demo.
- Anonymous `POST /demo/api/replay` returned 200 with the recorded upheld
  verdict and the expected failed recorded-clip status.
- Anonymous `POST /demo/api/reset` returned 200.
- A separate cookie jar replayed independently and returned the same recorded
  outcome. Resetting visitor A did not change visitor B's result.
- No owner session, OBS credentials, Twitch operations, model request, chat
  message or stream capture was used. The replay endpoints use fixture-backed
  demo services and have no provider adapters.

## Fresh-human rehearsal still required

Use two human-owned Twitch accounts and separate browser profiles. Existing
Twitch accounts are sufficient; fresh onboarding means new to this Receipts
deployment. The nrynss owner already completed hosted sign-in and OBS pairing,
so only a second owner is needed for the remaining isolation rehearsal.
On the deployed HTTPS origin, each owner signs in, verifies the displayed
channel identity, creates a fresh four-character OBS pairing code and confirms
it from their own OBS browser source. Verify each source shows only its own
channel. Revoke owner A's device through A's controls and verify A's device
loses snapshot and stream access while B's source continues. Complete hosted playback or truthful transcript fallback under T6.1. The
actual capture/overlay audio-feedback rehearsal belongs to T6.2. Record the result and limitations without tokens,
codes, cookies, transcript content or other secrets.

This rehearsal is deliberately outstanding: production account/device state
was not changed by the anonymous smoke check, and synthetic account seeding or
direct SQL auth injection cannot substitute for human owner acceptance.

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

## Human-owner acceptance procedure

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

The anonymous smoke alone does not satisfy this procedure. Human-owner
results and remaining limits are recorded below. Synthetic account seeding or
direct SQL auth injection cannot substitute for human owner acceptance.

## Hosted human-owner and device checks

The owner completed hosted OAuth onboarding for `nrynss` and `nrynproton`.
Read-only ledger inspection verified both connected accounts have distinct
channels. Existing `nrynss` OBS pairing was owner-confirmed earlier.

The owner confirmed a temporary browser-bound read-only source from
`nrynproton` setup using the four-character pairing flow. The continuously
polling receiver collected the resulting device cookie into a private temporary
cookie jar. Read-only comparison of its credential hash verified exactly one
active device bound to `nrynproton`. The credential and hash are not recorded.

- `/auth/device` returned 204 before revocation.
- `/api/overlay` returned 503 with the runtime unavailable because the channel
  was offline; this is not evidence of populated cross-channel overlay isolation.
- Device access to private health, snapshot and control mutation returned 403;
  the owner device-management endpoint returned 401. These were application
  denials, not Cloudflare challenges.
- The owner revoked the temporary `nrynproton` sources through setup. The same
  device cookie then received 401 from `/auth/device`, `/api/overlay` and
  `/api/events`.
- Read-only ledger inspection confirmed zero active `nrynproton` sources and
  the original active `nrynss` source retained.

The first test receiver missed the short collection window after confirmation;
that attempt is not counted as completed device acceptance. The retry used an
automatically polling receiver. Both temporary sources were revoked afterward.
No synthetic accounts, direct authorization injection, paid calls, Twitch chat,
stream capture or production receipt edits were used. Temporary test cookie
jars were removed after revocation verification.

The owner confirmed the hosted recorded OBS fixture displays the promise and
truthful transcript fallback. Remaining acceptance includes populated live
channel-data isolation. A temporary HTTP source verifies
device authorization but does not establish a second owner's real OBS playback.
T6.1 subsequently completed its hosted deployment, fixture fallback and actual
backup/restore checks. Populated channel-data isolation remains outstanding
for full TA.4 completion.

## Completion of populated hosted acceptance

The subsequent owner-authorized live run populated nrynss with a real captured
receipt and ready Twitch clip. While that channel was active, the separately
signed-in nrynproton control dock did not expose the claim or owner actions;
its owner-scoped private endpoints remained runtime-unavailable rather than
returning nrynss data. The complete run and synthetic-promise disclosure are
in [live acceptance evidence](t62-live-acceptance.md).

After issue 2's detection repair, the owner approved a real private review.
The original paired OBS source displayed the saved receipt after refreshing
its page, without re-pairing. This completes the previously outstanding
populated isolation and hosted OBS-display acceptance for TA.4, together with
the earlier onboarding, device revocation, anonymous-demo and recovery checks.
OBS video playback and the remaining full rehearsal steps are tracked under
T6.2 rather than inferred from a visible receipt.

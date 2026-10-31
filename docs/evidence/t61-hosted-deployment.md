# Hosted deployment evidence

Receipts is deployed at https://receipts.nryn.dev using the existing shared
Hetzner Docker proxy network and Traefik labels. The initial application image was built from source `002a742`; the current
image is `receipts:99d550d`, which includes four-character pairing codes and
the generation-bound clip worker.
Deployment uses the plain Docker pattern introduced in `f110d93`, with required
transcription and embedding settings added after initial startup validation.

- Private configuration was transferred over SSH standard input into a root-owned
  mode-0600 env file. The server generated its own token-encryption key.
- Docker reports the Receipts container healthy. Public `/healthz` returns only
  process status. Unauthenticated `/api/health` and `/api/snapshot` return 401.
- Both public HTTPS and the origin certificate were checked. The origin
  certificate covers `receipts.nryn.dev` and is issued by Let's Encrypt.
- Browser OAuth sign-in returned to hosted setup, connected as `nrynss`, with
  verified offline status and the hosted OBS URL.
- The nine existing containers remained running throughout deployment.
- A protected hosted backup completed and restarted only Receipts. The owner
  browser session and connected account recovered after reload.
- The independent offline gate and plain Docker SQLite recovery fixtures passed.

The raw Python HTTP client encountered Cloudflare's 403 challenge; curl and the
normal browser verified the service without weakening Cloudflare protections.

The owner confirmed adding the hosted OBS source, entering its code and
confirming pairing. The owner confirmed the hosted recorded fixture promise and transcript
fallback are visible in actual OBS. Hosted device revocation subsequently
passed in the two-owner check. Real clip playback is not claimed. The four-character pairing-code change is implemented separately with bounded
allocation and confirmation attempts; existing paired devices remain valid.
No paid model calls, stream capture or chat messages were performed during
these deployment checks. Automatic stamps remain disabled.

The four-character change passed the full offline gate and auth race checks.
The local mounted pairing fixture displayed the four-character sample and
two-minute expiry. A protected backup preceded the image switch; the previous
image is retained for rollback. The deployed pairing-start endpoint returned
exactly four characters from the readable alphabet, public health returned 200,
and private health returned 401. The existing active OBS device record remained
present after the update. No fresh owner/OBS pairing rehearsal is claimed for
this code change.

The owner supplied a screenshot and explicitly confirmed that the sample
promise and transcript fallback are visible after opening the hosted
`/?fixture=evidence` source in OBS. This is recorded-fixture rendering evidence,
not a real Twitch clip or live-channel receipt acceptance.

The principal ran the actual hosted restore using a freshly created backup,
after confirming there were no active live sessions. Restore recreated the
container and waited for healthy status. SQLite integrity passed; both real
owner accounts remained connected, nrynss retained one active device, and
nrynproton retained zero active devices after revocation. The pre-restore
directory and previous image were retained for rollback. This verifies the
hosted account/device ledger; receipt and overdue-appeal recovery are covered
by the offline application and real-SQLite fixtures.

The production clip-worker composition gap was fixed and deployed from
`99d550d` after independent full-gate and uncached startup/clip race checks.
Integration tests use real SQLite and mock Helix/EventSub for creation,
readiness, completion events, channel scope, restart deduplication and bounded
attachment cleanup. A protected backup preceded the update; the old image is
retained. The new container is healthy, with no active live session, no queued
clip jobs and no recorded paid charges. Real clip creation is not claimed by
these offline or deployment checks.

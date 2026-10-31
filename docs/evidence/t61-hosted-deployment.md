# Hosted deployment evidence

Receipts is deployed at https://receipts.nryn.dev using the existing shared
Hetzner Docker proxy network and Traefik labels. The running application image
was built from source `002a742`; deployment scripts use the plain Docker pattern
introduced in `f110d93`, with required transcription and embedding settings
added after the initial startup validation identified their absence.

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

Hosted OBS pairing, playback/fallback and revocation acceptance remain pending.
No paid model calls, stream capture or chat messages were performed during
these deployment checks. Automatic stamps remain disabled.

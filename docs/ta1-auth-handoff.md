# TA.1 authentication handoff

## Hosted configuration

`RECEIPTS_AUTH_ENABLED` defaults to `false`. Fixture mode does not resolve any
Twitch auth secrets or call a provider. Hosted auth is enabled explicitly in
live mode and requires `RECEIPTS_PUBLIC_ORIGIN` to use HTTPS, `TWITCH_CLIENT_ID`,
`TWITCH_CLIENT_SECRET`, `RECEIPTS_TOKEN_ENCRYPTION_KEY`, the existing OpenAI
configuration, and the model settings. The encryption key is base64 encoding
of exactly 32 random bytes. Keep it in the operator secret store, separate from
SQLite and its backups. Keep the same key available after restart; losing it
makes stored Twitch grants unreadable and requires owners to reconnect.

The callback URL is derived as
`<RECEIPTS_PUBLIC_ORIGIN>/auth/twitch/callback` and must match the URL
registered in the Twitch application. `GET /auth/twitch` starts authorization;
`GET /auth/twitch/callback` completes it; `POST /auth/logout` revokes the
current browser session. Mutating API handlers should call
`Service.ValidateCSRF`.

## Storage and checks

The account migration adds `accounts`, `browser_sessions`, and `oauth_states`
to the ledger migration stream. A Twitch subject maps to one account and one
existing channel row, so another login for that subject resumes the same local
IDs. Access and refresh tokens are AES-GCM encrypted in SQLite. The cookie
session token, CSRF token, OAuth state and browser binding are stored only as
SHA-256 digests. Session expiry and revocation are checked against SQLite on
every request. Login rotates the browser session; logout revokes it. The
browser session and Twitch authorization have separate states: a revoked or
invalid refresh grant changes the account connection state to
`reconnect_required` while leaving the browser able to start sign-in again.

The refresh mutex serializes refreshes per account inside the single Receipts
process. `AccessToken` refreshes after token expiry, and provider adapters can
call `RefreshAfterUnauthorized` after a Helix 401; a request carrying a token
that another goroutine already replaced receives the current token without
reusing the rotated refresh token. The new Keel `sqlite.DB.BeginWrite` API is
used for the state, account, and session transactions. The operator encryption key must remain stable
across restarts and key rotation currently requires reconnecting owners.

Keel identity was reviewed before implementation. Its existing signed cookie
contains the session ID and has no server-side expiry check, and its OIDC
provider returns a subject rather than Twitch's scoped access and refresh
tokens. Those contracts cannot provide this task's opaque expiring cookie and
scoped-token rotation, so the app owns those rows while continuing to use
Keel SQLite, migration, and ID APIs.

Offline provider fakes cover same-account login, invalid state, client and
grant rejection, session rotation/expiry, CSRF, encrypted restart recovery,
refresh serialization, and revoked/changed identity handling. Live Twitch
consent and hosted HTTPS behavior remain separate acceptance checks.

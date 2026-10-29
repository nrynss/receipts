# Hosted accounts and judge onboarding scope

Status: TA.1 identity/sessions implemented in `ed2c7d6`; TA.2–TA.4 remain planned. Shared channel runtimes, hosted onboarding, OBS pairing and the anonymous demo are not implemented. Hosted readiness is not claimed.

## Outcome

A judge opens Receipts and either tries a clearly labelled recorded demo immediately or signs in with Twitch to connect their own channel. A streamer can configure OBS and operate their channel without developer assistance. Viewers keep using Twitch chat without a Receipts account.

The core product remains promise → receipt → contradiction → stamp → appeal → verdict. Accounts make that experience accessible; they are not a separate social product.

## Smallest complete release

- One Twitch identity is one Receipts account and owns its own broadcaster channel. Resolve ownership from verified Twitch identity, never a typed channel name. No passwords or email collection.
- Multiple accounts may coexist on one hosted installation. Use one Go binary and persistent SQLite; each active channel has its own supervised runtime, event publisher, sequence/epoch, live-session authority, transcript buffer, cooldowns and spend accounting.
- A signed-in private dashboard shows connection health, live/listening state, stamp pause, usage and OBS setup, alongside the existing planned receipt controls.
- Setup while offline works. Listening requires explicit enablement and an authoritative live session. Stop listening cancels capture and work; pausing stamps is a separate action.
- A public recorded demo needs no login, credentials, live broadcast or provider calls. Playback uses the T1.4 driver and the real P2 application services, with full scenario acceptance in T2.4. Each visitor's interactive demo is isolated, bounded and disposable; it cannot modify live channels. Shared immutable fixture media is allowed.
- Hosted model usage is operator-funded with enforced per-channel and installation-wide caps. Capacity exhaustion is visible and queues or refuses activation without silently starting work. Operators configure finite concurrency and spend limits before release; unlimited public capture is out of scope.

## Streamer journey

1. Landing page offers “Try recorded demo” and “Connect Twitch”.
2. Twitch consent returns to the server; cancellation and denied permissions show a recoverable setup state.
3. Server verifies provider identity, app identity and grants, creates or resumes the account and its owned channel, then establishes a browser session.
4. Setup shows required grants, connection state and how to add an OBS Browser Source. No client secrets, Twitch tokens or developer console steps are required from the streamer.
5. Streamer pairs the OBS browser source using a short-lived, single-use code and confirms the matching code in their authenticated dashboard. The server binds a durable read-only device session to that channel. The copied OBS URL contains no credential. Test persistence in actual OBS across refresh and restart; this is a release gate.
6. Streamer explicitly enables listening. Offline, missing-grant, capacity and budget states explain the next action. Reconnection restores the current channel state without duplicate stamps.
7. Dashboard can revoke an OBS device, sign out, disconnect Twitch, or delete the account and channel data. Sign-out ends the browser session; disconnect also stops capture and provider activity. Deletion covers dependent records and follows documented backup retention.

## Identity and access boundaries

Use server-side authorization-code OAuth with state bound to the initiating browser. Keep access/refresh tokens encrypted at rest using an operator-managed key outside SQLite and backups; redact tokens and callback codes from logs. Refresh and validate provider tokens, persist rotated credentials atomically and require reconnect when authorization is lost. The existing implicit local probe is not production login.

Use opaque server-side browser sessions with Secure, HttpOnly, SameSite cookies, expiry, session rotation at login and CSRF protection for changes. Resolve account/channel access server-side on every request. MVP has one owner role; moderator delegation is deferred. Provider authorization and the Receipts browser session have separate lifecycles.

An OBS device may read only its own public overlay snapshot and stream. It cannot read private health/review/transcript data or operate controls. Pairing codes expire, are rate-limited and cannot be reused; device credentials are revocable and stored hashed server-side. Demo sessions cannot access owner or OBS APIs.

Channel IDs in payloads and URLs are selectors, never proof of ownership. All storage, cached retrieval, event subscription, background job and budget operations must carry validated channel context. The present global API credential pair is insufficient for hosted operation and must become an authorization boundary that resolves these identities.

Implementation task status and evidence live in [the TA phase document](../dev-diary/PHASE-A-accounts.md).

## Build sequence and acceptance

| Task | Requires | Scope | Observable acceptance |
|---|---|---|---|
| TA.1 Identity and sessions | T1.1, T1.2 | Account/channel ownership migration, OAuth callback, encrypted provider credentials, sessions, refresh/revoke | Repeat login resumes the same account; invalid state, wrong app identity and missing grants fail closed; no secrets in logs or responses |
| TA.2 Channel authorization and runtime isolation | TA.1, T1.3 | Channel-scoped API authorization, publishers and supervised lifecycle, per-channel/global limits | Two accounts can run concurrently; neither can read, control, subscribe to or consume the other's budget/state; stopping one leaves the other running |
| TA.3 Onboarding and OBS pairing | TA.2, T3.1 | Private setup/dashboard, pairing, device revocation, reconnect states | New owner completes setup without developer help; actual OBS retains pairing across restart; expired/reused codes and revoked devices fail |
| TA.4 Judge demo and hosted acceptance | TA.2, TA.3, T1.4, T2.4, T3.3, T6.1 | Anonymous isolated recorded demo, deployed HTTPS checks, lifecycle/deletion acceptance | Two anonymous demos cannot affect each other or live channels; provider calls stay at zero; fresh Twitch account and two-owner isolation rehearsal pass |

These IDs supplement the existing phases without renumbering completed work. TA.1 is landed; TA.2 is ready to begin. TA.2 must settle before live adapters are integrated into a shared hosted runtime. T3.2 uses owner sessions rather than a shared control secret. T6.1 deploys the candidate after TA.3; TA.4 verifies the deployed experience and gates T6.3 judge readiness. Existing offline product-loop work can proceed in parallel against explicit channel context.

## Deferred

Billing, subscriptions, organization workspaces, moderator invitations, multiple owned channels per account, social profiles, public searchable ledgers, cross-channel lore and a dedicated shared bot identity are deferred. They do not improve the first judge or streamer journey enough to justify their access and operational complexity. No horizontal scaling or database replacement is required for this release.

## Release evidence

Keep the full offline gate independent of providers. Add recorded OAuth/refresh failures and two-channel negative authorization tests, including guessed IDs, mixed-channel source evidence, SSE replay, delayed jobs and device revocation. Test session expiry, CSRF, refresh concurrency and restart recovery. Real Twitch/OBS acceptance remains separate and budgeted. Do not mark hosted readiness from fake transports alone.

Unresolved deployment inputs: public HTTPS origin, host capture capacity, configured active-channel limit and operator spend caps. These must be chosen and measured before hosted activation. No build-time schedule is assumed.

## Provider references

- [Twitch authorization-code flow](https://dev.twitch.tv/docs/authentication/getting-tokens-oauth/)
- [Token validation](https://dev.twitch.tv/docs/authentication/validate-tokens/)
- [Token refresh](https://dev.twitch.tv/docs/authentication/refresh-tokens/)

The hosted authorization design follows these provider contracts; exact grants and account eligibility must be verified when implementing the live adapters.

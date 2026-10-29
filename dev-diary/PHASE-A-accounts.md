# TA: Accounts and judge onboarding

**Goal:** Let judges try Receipts immediately and streamers connect their own channel safely.

**Status:** in-progress (1/4)

Read [the phase index](README.md), [account scope](../docs/accounts-scope.md), [product](../product.md) and [agent protocol](../AGENTS.md). The scope defines product boundaries; this document owns TA task status, dependencies and implementation evidence.

### TA.1: Identity and sessions

```yaml
requires: T1.1, T1.2
fixture-ok: yes
owns: internal/auth/, internal/store/migrations/, internal/settings/, cmd/receipts/, testdata/auth/
status: done
```

Implement Twitch authorization-code login, verified identity and one owned channel per account. Persist encrypted provider credentials and refresh rotation. Use opaque browser sessions, login rotation, expiry, CSRF protection and safe logout. Reconnect after revoked authorization; the local implicit probe is not production login.

**Done when:** Repeated login resumes the same account. Invalid state, wrong app identity, missing grants, expired sessions and CSRF attempts fail closed. Concurrent refresh cannot lose rotated credentials. No secrets appear in responses, fixtures or logs.

### TA.2: Channel authorization and runtime isolation

```yaml
requires: TA.1, T1.3
fixture-ok: yes
owns: internal/auth/, internal/api/, internal/events/, internal/runtime/, internal/store/, internal/settings/
status: not-started
```

Resolve validated owner or device channel context at every API boundary. Give each active channel its own publisher, epoch, session authority, cancellation, buffers, cooldowns and usage. Enforce per-channel and global capacity/spend limits; reject or queue activation visibly. Coordinate shared paths with live adapter and product-loop tasks.

**Done when:** Two owners cannot read, mutate, subscribe to, replay or consume the other channel’s state or budget. Guessed IDs, delayed jobs and mixed-channel evidence fail. Stopping or disconnecting one channel leaves the other running. Restart preserves ownership and finite limits.

### TA.3: Onboarding and OBS pairing

```yaml
requires: TA.2, T3.1
fixture-ok: yes
owns: web/src/lib/account/, web/src/routes/, internal/auth/, internal/api/, docs/obs.md
status: not-started
```

Build connection/setup dashboard with offline, live, listening, paused, missing-grant and capacity states. Pair OBS through an expiring single-use code confirmed by the owner. Keep the copied browser-source URL free of credentials. Persist revocable read-only device sessions; store device credentials hashed. Implement disconnect and account deletion, including dependent records and documented backup retention.

**Done when:** A new owner completes setup without developer assistance. Pairing codes expire and cannot be reused; revoked devices immediately lose snapshot and stream access. Device sessions cannot reach private controls, review or transcript data. Actual OBS pairing survives refresh/restart. Disconnect stops provider activity; deletion removes the account’s active data.

### TA.4: Judge demo and hosted acceptance

```yaml
requires: TA.2, TA.3, T1.4, T2.4, T3.3, T6.1
fixture-ok: yes
owns: internal/replay/, web/src/lib/demo/, web/src/routes/, docs/evidence/, docs/operations.md
status: not-started
```

Offer a clearly labelled recorded demo without login. Run the real application services in bounded disposable visitor-isolated state with no provider calls. Verify the deployed HTTPS candidate, fresh Twitch onboarding, two-owner isolation and lifecycle behavior. Record limitations and evidence separately from offline fixtures.

**Done when:** Two anonymous demo visitors cannot affect each other or live channels. Demo provider calls remain zero even when host credentials exist. Fresh-account onboarding, two-owner isolation, revocation and OBS acceptance pass on the deployed origin; evidence is available before T6.3.

## Phase integration

TA.1 is landed in `ed2c7d6`; T1.4 replay foundation is landed in `ced2280`. P2 continues against explicit channel context. TA.2 gates shared P4/P5 runtime integration and T3.2 owner controls. TA.3 joins T3.1 and precedes T6.1 deployment. TA.4 verifies that deployment and gates T6.3 judge readiness. Fixture readiness permits offline implementation; real Twitch/OBS acceptance still requires separate budgeted live evidence. No dependency runs from T6.1 back to TA.4.

## Exit criteria

- [ ] Each task has evidence against its done criteria.
- [x] Offline gate passes without keys, network or live broadcasts at `ed2c7d6`.
- [ ] Two-owner and anonymous-demo isolation are verified.
- [ ] Twitch/OBS checks are separately recorded on the deployed origin.
- [ ] Revocation, disconnect, deletion and operational limits are documented.

## Handoff log

TA.1 is reviewed from `50ced59` and integrated with startup in `ed2c7d6`. Principal-thread full offline gate and uncached auth/settings/store/startup race tests passed. TA.2 is the next account task; channel-scoped runtime/API authorization and onboarding UI are not implemented. Public HTTPS origin, active-channel capacity and operator spend limits must be configured before hosted activation. Billing, moderator delegation and multiple owned channels per account remain deferred.

Review fixed login/refresh/revocation ordering, durable rotated-token persistence despite caller cancellation or temporary validation outage, subject-bound encryption, and session issuance after revocation. Startup and cancellable hourly maintenance validate connected grants; invalid authorization revokes browser sessions. Provider tests are offline; real Twitch consent at the hosted HTTPS origin remains TA.4 acceptance. See [auth handoff](../docs/ta1-auth-handoff.md).

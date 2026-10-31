# TA: Accounts and judge onboarding

**Goal:** Let judges try Receipts immediately and streamers connect their own channel safely.

**Status:** in-progress (3/4)

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
status: done
```

Resolve validated owner or device channel context at every API boundary. Give each active channel its own publisher, epoch, session authority, cancellation, buffers, cooldowns and usage. Enforce per-channel and global capacity/spend limits; reject or queue activation visibly. Coordinate shared paths with live adapter and product-loop tasks.

**Done when:** Two owners cannot read, mutate, subscribe to, replay or consume the other channel’s state or budget. Guessed IDs, delayed jobs and mixed-channel evidence fail. Stopping or disconnecting one channel leaves the other running. Restart preserves ownership and finite limits.

### TA.3: Onboarding and OBS pairing

```yaml
requires: TA.2, T3.1
fixture-ok: yes
owns: web/src/lib/account/, web/src/routes/, internal/auth/, internal/api/, docs/obs.md
status: done
```

Build connection/setup dashboard with offline, live, listening, paused, missing-grant and capacity states. Pair OBS through an expiring single-use code confirmed by the owner. Keep the copied browser-source URL free of credentials. Persist revocable read-only device sessions; store device credentials hashed. Implement disconnect and account deletion, including dependent records and documented backup retention.

**Done when:** A new owner completes setup without developer assistance. Pairing codes expire and cannot be reused; revoked devices immediately lose snapshot and stream access. Device sessions cannot reach private controls, review or transcript data. Actual OBS pairing survives refresh/restart. Disconnect stops provider activity; deletion removes the account’s active data.

### TA.4: Judge demo and hosted acceptance

```yaml
requires: TA.2, TA.3, T1.4, T2.4, T3.3, T6.1
fixture-ok: yes
owns: internal/replay/, web/src/lib/demo/, web/src/routes/, docs/evidence/, docs/operations.md
status: in-progress
```

Offer a clearly labelled recorded demo without login. Run the real application services in bounded disposable visitor-isolated state with no provider calls. Verify the deployed HTTPS candidate, fresh Twitch onboarding, two-owner isolation and lifecycle behavior. Record limitations and evidence separately from offline fixtures.

**Done when:** Two anonymous demo visitors cannot affect each other or live channels. Demo provider calls remain zero even when host credentials exist. Fresh-account onboarding, two-owner isolation, revocation and OBS acceptance pass on the deployed origin; evidence is available before T6.3.

The anonymous recorded-demo implementation may proceed now using completed T1.4/T2.4 services and TA.2 isolation. T6.1 remains a binding prerequisite for deployed-origin acceptance and full TA.4 completion. A separate Luna worktree owns this independent demo portion; no hosted acceptance is claimed.

## Phase integration

TA.1 is landed in `ed2c7d6`; T1.4 replay foundation is landed in `ced2280`. P2 continues against explicit channel context. TA.2 gates shared P4/P5 runtime integration and T3.2 owner controls. TA.3 joins T3.1 and precedes T6.1 deployment. TA.4 verifies that deployment and gates T6.3 judge readiness. Fixture readiness permits offline implementation; real Twitch/OBS acceptance still requires separate budgeted live evidence. No dependency runs from T6.1 back to TA.4.

## Exit criteria

- [ ] Each task has evidence against its done criteria.
- [x] Offline gate passes without keys, network or live broadcasts at `ed2c7d6`.
- [ ] Two-owner and anonymous-demo isolation are verified.
- [ ] Twitch/OBS checks are separately recorded on the deployed origin.
- [ ] Revocation, disconnect, deletion and operational limits are documented.

## Handoff log

TA.1 is reviewed from `50ced59` and integrated with startup in `ed2c7d6`. Principal-thread full offline gate and uncached auth/settings/store/startup race tests passed. TA.2 runtime/API authorization is reviewed from `7c463e1`; TA.3 onboarding and OBS acceptance are complete. Onboarding UI and OBS pairing are implemented; trusted live adapter activation remains pending P4.4. Public HTTPS origin, active-channel capacity and operator spend limits must be configured before hosted activation. Billing, moderator delegation and multiple owned channels per account remain deferred.

Review fixed login/refresh/revocation ordering, durable rotated-token persistence despite caller cancellation or temporary validation outage, subject-bound encryption, and session issuance after revocation. Startup and cancellable hourly maintenance validate connected grants; invalid authorization revokes browser sessions. Provider tests are offline; real Twitch consent at the hosted HTTPS origin remains TA.4 acceptance. See [auth handoff](../docs/ta1-auth-handoff.md).

### TA.2 acceptance

Reviewed candidate `7c463e1` composes owner/device middleware, channel-local publishers and runtime generations, scoped ledger reads and durable Keel budgets on the same SQLite file. The principal thread contributed budget/read-model/composition tests and independently verified the full gate plus uncached auth/API/runtime/settings/store/commands/startup race checks. Tests cover guessed authority, two-owner data and SSE isolation, device read-only restrictions, CSRF, revocation/session expiry, capacity held until workers stop, pre-cancelled work, activation/revocation races and persistent spend caps. Void, review and receipt decisions now use the completed P2 services. P4 must call the verified activation seam; no real live runtime is enabled just by login. See [runtime handoff](../docs/ta2-runtime-handoff.md).

TA.3 implementation is reviewed from `cb0bd7a` and `6b6c63c`: account/OBS setup, persistent revocable device cookies, owner deletion and typed status are implemented. Offline gate and focused race tests pass; dashboard and pairing panel were visually reviewed. A build-tagged loopback acceptance harness now supports a real OBS pairing rehearsal with a synthetic account and no provider calls; automated harness evidence is recorded separately from owner-reported OBS observations. Actual local OBS acceptance passed: the owner confirmed source refresh, OBS restart and server restart without re-pairing, restored appeal countdown/counts and immediate clearing after revocation. Synthetic process-restart tests verify persistent device authorization. See [OBS evidence](../docs/evidence/t33-ta3-obs.json). Fresh real-owner onboarding on the deployed origin remains TA.4. T4.4 now supplies verified session lifecycle activation; actual audio capture remains unavailable until T5.1.

TA.4 anonymous demo implementation is reviewed and integrated in `431ca1e`: separate bounded disposable visitor ledgers run real recorded product services without provider clients, and replay/reset/keyboard browser checks pass. Regression tests cover two-visitor isolation, no provider calls with credentials configured, expiry during a request, contention and shutdown. Full TA.4 completion remains dependent on T6.1 and fresh deployed-owner/two-owner acceptance.

### Four-character OBS pairing code

Owner feedback from hosted setup: the pairing code was too long. Codes now
contain exactly four readable uppercase letters/digits, excluding I, O, 0 and 1.
Setup accepts lowercase input. Codes expire after two minutes and remain
single-use and bound to the requesting OBS browser. Allocation retries active
code collisions without replacing an existing source. At most 32 requests may
be pending; confirmation is limited to five attempts per owner account and
20 globally per minute. Browser-session rotation does not reset the owner limit.
Existing durable paired devices remain valid. Regression checks cover code
format, collisions/exhaustion, expiry, browser binding, CSRF, single use,
account limits, session rotation and recovery after the rate-limit window.

TA.4 anonymous hosted smoke verified recorded demo replay/reset behavior and
private API protection. Results, completed two-owner device checks and remaining acceptance are
in [hosted smoke evidence](../docs/evidence/ta4-hosted-smoke.md). The principal
verified hosted nrynss sign-in and owner-confirmed OBS pairing separately.
TA.4 remains in progress for populated channel-data isolation. Hosted
owner-to-device binding and revocation passed for nrynproton while the original
nrynss source remained active. T6.1 completed hosted deployment, recorded OBS
fallback and actual backup/restore acceptance; it no longer blocks TA.4.

# P4: Live Twitch chat and evidence

**Goal:** Replace recorded Twitch adapters while keeping the proven product loop and explicit failure states.

**Status:** not-started

Read [the phase index](README.md), [product spec](../product.md) and [agent protocol](../AGENTS.md) first. Task-level dependencies below are binding.

### T4.1: Authorization and channel setup

```yaml
requires: T0.2, T1.3
fixture-ok: yes
owns: internal/twitch/auth/, internal/api/, web/src/lib/setup/, docs/setup.md
status: not-started
```

Implement the verified Twitch authorization flow for one channel and the chosen bot identity. Validate callback state, bind the authorized broadcaster, refresh/revoke credentials safely and expose connection health. Provide a private overlay URL and a separate control login. Configuration and environment setup for our channel are sufficient initially; public multi-channel onboarding is deferred. Follow repository secret rules for any runtime token persistence.

**Done when:** Recorded callbacks cover success, denial, state mismatch and expired credentials. A bounded live-tagged check proves the authorized account can perform the required operations without exposing tokens.

### T4.2: EventSub and bot transport

```yaml
requires: T4.1, T2.4
fixture-ok: yes
owns: internal/twitch/eventsub/, internal/twitch/chat/, testdata/twitch/
status: not-started
```

Implement verified welcome/subscription, keepalive, reconnect and revocation behavior. Deduplicate message IDs and map chat roles from trusted Twitch events. Route commands into the same services used by replay. Bound retries, bot replies and reconnect backoff; report authorization loss instead of silently looping.

**Done when:** Fixture reconnects and duplicate deliveries cause one product action. A bounded real-channel check proves !receipt, appeal votes, moderator controls and !lore arrive through Twitch.

### T4.3: Clip jobs and playback

```yaml
requires: T4.1, T2.1, T3.1
fixture-ok: yes
owns: internal/twitch/clips/, internal/receipt/, testdata/twitch/
status: not-started
```

Use the proven clip API contract. Persist pending work, poll with bounded retries and represent ready, unavailable and failed separately. Correlate clip timing with transcript evidence; never imply a late clip contains words it missed. Recheck the embed on the deployed origin in OBS and verify audio does not create a feedback loop into listening.

**Done when:** Recorded delayed readiness, rate limits and permanent failure preserve the receipt. A real clip is visible in OBS or the documented transcript fallback explains why video is unavailable.

### T4.4: Live-session authority

```yaml
requires: T0.3, T4.2
fixture-ok: yes
owns: internal/session/, internal/twitch/live/, testdata/twitch/
status: not-started
```

Combine verified live-status signals with bounded freshness and startup checks. Bind audio and pending results to a session identity. Offline or unknown status stops listening; an old session cannot publish after a new one starts. Expose the state to streamer controls. Explicit mic enablement never carries into another live session.

**Done when:** Offline, stale confirmation, reconnect, stream restart and delayed model results are covered with fixtures. A live end-of-stream rehearsal confirms capture stops.

## Exit criteria

- [ ] Every task above has evidence against its done criteria.
- [ ] The offline gate passes on the candidate revision.
- [ ] Live checks are separately identified, budgeted and never part of the offline gate.
- [ ] Outstanding limitations and downstream contract changes are recorded.

## Handoff log

### What exists now

Planning only. No implementation is claimed.

### Evidence and decisions

Record task IDs, relevant revision, checks and artifact paths when work lands. Never record secrets.

### Notes for the next task

Record unresolved blockers, contract changes and the next unblocked task.

# P4: Live Twitch chat and evidence

**Goal:** Replace recorded Twitch adapters while keeping the proven product loop and explicit failure states.

**Status:** in-progress (1/4)

Read [the phase index](README.md), [product spec](../product.md) and [agent protocol](../AGENTS.md) first. Task-level dependencies below are binding.

### T4.1: Connect authorized channel adapters

```yaml
requires: T0.2, T1.3, TA.2
fixture-ok: yes
owns: internal/twitch/connection/, internal/api/, testdata/twitch/, docs/setup.md
status: done
```

Consume TA.1 OAuth credentials through TA.2 validated channel context and runtime supervision. Do not implement a second login/token store. Bind each EventSub/Helix adapter to the verified broadcaster, use the shared refresh/validation lifecycle and report connection health and authorization loss. The owner uses their own broadcaster identity in the initial release; a shared bot is deferred. TA.3 owns setup and OBS pairing.

**Done when:** Recorded adapters prove channel binding, token expiry/refresh, authorization loss and runtime cancellation; an adapter cannot use another owner’s grant. A bounded live-tagged check proves the authorized account can perform the required operations without exposing tokens.

### T4.2: EventSub and bot transport

```yaml
requires: T4.1, T2.4
fixture-ok: yes
owns: internal/twitch/eventsub/, internal/twitch/chat/, testdata/twitch/
status: in-progress
```

Implement verified welcome/subscription, keepalive, reconnect and revocation behavior. Deduplicate message IDs and map chat roles from trusted Twitch events. Route commands into the same services used by replay. Bound retries, bot replies and reconnect backoff; report authorization loss instead of silently looping.

**Done when:** Fixture reconnects and duplicate deliveries cause one product action. A bounded real-channel check proves !receipt, appeal votes, moderator controls and !lore arrive through Twitch.

### T4.3: Clip jobs and playback

```yaml
requires: T4.1, T2.1, T3.1
fixture-ok: yes
owns: internal/twitch/clips/, internal/receipt/, testdata/twitch/
status: in-progress
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

T4.1 adapter integration and bounded live chat/clip acceptance are complete. P0 proved bounded Twitch capability; TA.1 OAuth/session lifecycle is implemented in `ed2c7d6`. These do not establish a running multi-channel EventSub/clip product pipeline.

### Evidence and decisions

Record task IDs, relevant revision, checks and artifact paths when work lands. Never record secrets.

### Notes for the next task

T4.1 consumes TA.2 through `internal/auth` token access and unauthorized-refresh hooks. Never bypass channel authorization using caller-supplied IDs.

T4.1 implementation is reviewed and integrated in `2627aea`: scoped Helix/EventSub requests, bounded owner status observation, one refresh retry and generation/token-safe authorization loss pass offline and race checks. The authorized live adapter writer probe also passed: validation matched nrynss and required scopes, the channel was live, one chat message was accepted, and one clip ID was returned in four Twitch requests. Sanitized success evidence is in `docs/evidence/t41-adapter-live-write.json`; clip readiness was not polled. The preceding offline attempt is retained in `docs/evidence/t41-adapter-live.json`.

The opt-in adapter-bound writer acceptance validates the grant (including the nrynss login) and channel binding, confirms live status, then permits exactly one fixed chat message and one clip creation under an eight-request cap. The live harness uses an in-memory resolver/token provider and does not establish hosted account persistence or live refresh; offline tests cover those paths. EventSub subscription and receive transport remain T4.2 acceptance.

T4.2 transport and T4.3 clip jobs are assigned to separate Luna worktrees from `85c69b8`. The principal thread owns integration and task status. Live acceptance remains distinct from offline implementation, and T4.4 still owns verified listening activation.

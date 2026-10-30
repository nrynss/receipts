# P3: OBS overlay and streamer controls

**Goal:** Make the core moment readable and controllable on a real broadcast canvas.

**Status:** in-progress (2/3)

Read [the phase index](README.md), [product spec](../product.md) and [agent protocol](../AGENTS.md) first. Task-level dependencies below are binding.

### T3.1: Overlay states and playback

```yaml
requires: T1.3
fixture-ok: yes
owns: web/src/lib/overlay/, web/src/routes/, testdata/contracts/
status: done
```

Build transparent idle, stamp, evidence, appeal and verdict states using Svelte 5 and Chaaya. Show the exact claim, credited viewer and receipt age. Use original visual assets and an intentional stamp animation. Provide reduced-motion behavior and readable contrast. Clip playback has a visible transcript fallback for unavailable, blocked or pending embeds. Revalidate real Twitch playback in P4; fixture playback is not evidence of embed compatibility.

**Done when:** Fixture events display the whole sequence at broadcast size. Text stays readable in a scaled viewer preview, and a failed clip never leaves a blank or stuck stamp.

### T3.2: Streamer control surface

```yaml
requires: T2.3, T3.1, TA.2
fixture-ok: yes
owns: web/src/lib/control/, web/src/routes/, internal/api/
status: done
```

Provide a private control page usable as an OBS dock: Admit, Appeal, Context changed with a reason, pause/resume stamps, void and quiet review. Show live/audio state distinctly from stamp pause and expose a listening stop control. Keep sensitive transcript material out of the overlay stream. Authentication and role checks remain server-side.

**Done when:** An authenticated operator can resolve the scripted case. Keyboard actions work, pending actions cannot double-submit, and an overlay read token cannot operate controls.

### T3.3: SSE recovery and OBS rehearsal

```yaml
requires: T2.4, T3.2
fixture-ok: yes
owns: web/src/lib/events/, internal/events/, docs/obs.md
status: not-started
```

Connect Keel stream to Chaaya createEventStream with snapshots and stable IDs. Show disconnected state privately; restore the current appeal and remaining time from server state. Document browser-source dimensions and an OBS setup rehearsal using fixture mode. Capture no tokens in screenshots.

**Done when:** Reconnect, refresh and server restart restore correct state without duplicate animation or a reset voting deadline. The recorded-input demo works as an OBS browser source.

## Exit criteria

- [ ] Every task above has evidence against its done criteria.
- [ ] The offline gate passes on the candidate revision.
- [ ] Live checks are separately identified, budgeted and never part of the offline gate.
- [ ] Outstanding limitations and downstream contract changes are recorded.

## Handoff log

### What exists now

T3.1 overlay states/playback components are reviewed from `090acd8`. T1.3 supplies TypeScript decoding/reconnect helpers and TA.1–TA.2 supply hosted authorization. Streamer controls, authenticated overlay transport and OBS pairing are implemented. Actual OBS/SSE recovery remains T3.3.

### Evidence and decisions

Record task IDs, relevant revision, checks and artifact paths when work lands. Never record secrets.

### Notes for the next task

TA.3 onboarding/OBS pairing is ready from T3.1 and TA.2. T3.2 requires T2.3. TA.3 builds pairing/setup alongside this phase; keep read-only OBS devices separate from owner sessions.

### T3.1 acceptance

The principal thread independently ran the complete gate and browser harness on reviewed `090acd8`. Mounted checks passed for pending/unavailable clips, simulated iframe error/timeout, state changes through idle, exact long text with bounded scrolling, timer cleanup and snapshot recovery without stamp animation. The 640×360 run also passed with reduced motion emulated. Evidence-state screenshots were visually inspected at 640×360 and 1920×1080; no saved image artifact is claimed. Credit and fallback were readable at both sizes. See [browser evidence](../docs/evidence/t31-overlay.md).

The default page stays transparent/idle; `?fixture=sequence` selects original visual fixtures. This is not the full anonymous product demo or production SSE composition. Real Twitch playback and OBS are not verified by simulated iframe events; saved claim text stays visible when an embed cannot report its cross-origin playback state. T3.3 and P4 retain live/OBS acceptance.

### T3.2 control dock handoff

The owner dock at `/control` reads account listening status and the active runtime's snapshot, health and private review queue. It sends owner actions through the existing CSRF-protected endpoints. Decision and review retries retain their action key across refreshes in the same tab, scoped to the authenticated owner and channel. The stored record contains a SHA-256 digest of the action payload and the key, never credentials, private review text or context reasons. Definitive 4xx refusals release the key; network and server failures retain it. Retry the same choice and reason; a different keyed action is blocked until the uncertain action succeeds. The dock does not enable live activation: it displays the backend's availability and exposes stop only while listening. Review claims and reasons remain private to the authenticated owner endpoint.

The offline gate and client tests cover pending submission and retry identity. Existing hosted API tests cover owner and device authorization. Principal browser rehearsal verified keyboard pause/resume, Admit, required context reasons, Context changed and review approval with original fixture data. The dock was visually inspected. Full offline gate and backend device-denial checks pass. Actual OBS/SSE recovery remains T3.3; no live activation is claimed.

The development-only `/control?fixture=dock` rehearsal uses original local fixture data and no provider calls. It is labelled as a fixture and is not enabled in production. Automated tests cover key retention across refresh, owner isolation, absence of the private reason in storage, and pending-action suppression.

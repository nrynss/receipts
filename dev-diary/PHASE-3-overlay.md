# P3: OBS overlay and streamer controls

**Goal:** Make the core moment readable and controllable on a real broadcast canvas.

**Status:** not-started

Read [the phase index](README.md), [product spec](../product.md) and [agent protocol](../AGENTS.md) first. Task-level dependencies below are binding.

### T3.1: Overlay states and playback

```yaml
requires: T1.3
fixture-ok: yes
owns: web/src/lib/overlay/, web/src/routes/, testdata/contracts/
status: not-started
```

Build transparent idle, stamp, evidence, appeal and verdict states using Svelte 5 and Chaaya. Show the exact claim, credited viewer and receipt age. Use original visual assets and an intentional stamp animation. Provide reduced-motion behavior and readable contrast. Clip playback has a visible transcript fallback for unavailable, blocked or pending embeds. Revalidate real Twitch playback in P4; fixture playback is not evidence of embed compatibility.

**Done when:** Fixture events display the whole sequence at broadcast size. Text stays readable in a scaled viewer preview, and a failed clip never leaves a blank or stuck stamp.

### T3.2: Streamer control surface

```yaml
requires: T2.3, T3.1, TA.2
fixture-ok: yes
owns: web/src/lib/control/, web/src/routes/, internal/api/
status: not-started
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

Planning only. No implementation is claimed.

### Evidence and decisions

Record task IDs, relevant revision, checks and artifact paths when work lands. Never record secrets.

### Notes for the next task

Record unresolved blockers, contract changes and the next unblocked task.

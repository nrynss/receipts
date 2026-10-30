# T3.1 overlay browser verification

Candidate: `090acd8`, reviewed in the principal Codex thread.

The full offline gate passes. The principal browser ran the mounted harness at `/tests/overlay.html` on a localhost Vite server after fixing its initially blocked module path. The harness is served only in development and does not widen Vite filesystem access. Its CSP blocks external iframe requests.

## Observed results

- PASS: unavailable/pending clips preserve the saved public claim.
- PASS: a ready iframe retains text fallback; simulated error and controlled timeout switch to unavailable.
- PASS: snapshot recovery is nonanimated; a fresh stamp selects animation.
- PASS: evidence, appeal, verdict and idle transitions update the mounted component.
- PASS: long claim/ID/viewer text stays exact, within viewport height and without horizontal overflow; excess content scrolls.
- PASS: unmount removes the overlay and clears clip deadlines.
- PASS: at 640×360 with reduced motion emulated, stamp animation is disabled and the same lifecycle checks pass.
- Visual inspection: the evidence state is readable at 640×360 and 1920×1080; public claim, viewer credit, receipt age and saved-claim fallback fit. Screenshot images were inspected in the browser tool; no file path is claimed.

## Limits

Only original visual fixtures and simulated iframe outcomes were used. Saved claim text is not presented as a raw transcript excerpt. Cross-origin video failure is not reliably observable from iframe load, so the fallback stays visible. No real Twitch embed, OBS, authenticated SSE composition or complete product appeal flow was verified. Those remain P4/T3.3/TA.4 acceptance.

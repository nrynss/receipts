# Mounted overlay checks

Run `npm run dev` and open `/tests/overlay.html` in a browser. The page runs component lifecycle assertions and reports PASS or FAIL. Its content security policy blocks iframe network requests; the deadline is controlled by the harness. Repeat with reduced motion emulated to check the animation opt-out. The harness is separate from the offline Vitest gate and does not verify Twitch playback or OBS.

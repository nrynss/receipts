# TU.2 handoff

TU.2 implements the broadcast overlay polish, public landing page, and recorded judge walkthrough.

## Evidence

- `/` renders the landing hero, private-detection approval explanation, three workflow steps, CTAs and a four-state interactive `<Overlay />` preview.
- The landing preview renders its default sample stamp in SSR, remains static (no live-arrival animation), responds to its narrow container, and labels the fixed fixture timestamp as recorded sample data.
- Recorded appeal previews use their fixture deadline to show a bounded duration at stage start; live countdowns continue using the authoritative deadline and wall clock.
- `/?fixture=sequence|stamp|evidence|appeal|verdict` remains a transparent overlay harness without human navigation chrome.
- `/demo/` keeps the visitor-isolated replay/reset API and mounts `<Overlay />` with four stage tabs, a structured replay timeline, aligned recorded claim, and a simulated `!lore` chat response. It makes no provider calls.
- Live stamp arrivals reveal evidence after 1.5 seconds; catch-up snapshots remain non-animated. Clip fallback avoids repeating the quote while an iframe is present, appeal shows the vote split, and full receipt IDs remain accessible while visually shortened.
- `GOCACHE=/tmp/receipts-go-cache ./tools/check.sh` passed: frontend lint, Svelte diagnostics, 29 frontend tests, production build, Go vet and Go tests. The worktree used a temporary link to installed frontend dependencies; that link was removed after each gate run.

Mounted-browser verification at 1920×1080 and 640×360, including reduced motion, remains for the parent task's CUA session.

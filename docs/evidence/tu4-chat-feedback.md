# TU.4 chat feedback evidence

## Implemented

- `!receipt` sends `🧾 Receipt taken (@<login>): "<claim>"` only after `ReplyCaptured` with a nonnil receipt. Per-user and global reply limits bound chat volume; the rendered message is valid UTF-8 and no longer than 450 bytes. Existing command deduplication and capture results are unchanged. Capture errors, non-capture outcomes, and a missing sender remain silent.
- `!lore` retains its scoreboard prefix and annotates each claim with its outcome: `(open)`, `(broken)`, `(overturned)`, `(context changed)`, or `(inconclusive)`. Claim rendering keeps the full item tag within the 450-byte UTF-8 budget.
- Production chat composition now connects the verified router to the existing appeal service, ledger lore service, and authority-bound Twitch connection sender. This composition wiring is outside TU.4's nominal router/lore ownership, but without it neither chat replies nor the existing appeal/lore routes reached their services in the live EventSub path.

## Offline evidence

- `internal/twitch/chat` covers the success gate, silent capture outcomes and errors, nil sender behavior, valid UTF-8, the byte ceiling, per-user throttling, and global throttling across users with recovery after its window.
- `cmd/receipts` tests the production router constructor and verifies that it binds the intelligence capture service, appeal and lore services using the ledger/runtime, and the authority-bound connection adapter as sender.
- `internal/lore` covers all outcome tags, retained scoreboard counts, UTF-8 validity, and the byte ceiling.
- `internal/twitch/eventsub` runs the synthetic acceptance sink through the real capture, appeal, pause/resume, lore, and chat-sender ports, asserting the receipt confirmation without Twitch access.

## Browser and live evidence

Parent mounted browser review verified home, demo, account setup, the dock fixture with Confirmed status/manual review, and chrome-free OBS and appeal fixture routes. The corrected recorded demo replay/reset and matching lore/verdict were verified in the combined TU.2/TU.3 review. See [overlay and setup evidence](tu2-tu3-browser.md) for responsive, reduced-motion and mounted overlay lifecycle checks. The independent full offline gate and targeted production/chat/lore/EventSub race checks passed. No Twitch chat send or paid provider call was made.

Final parent mounted overlay acceptance passed at 1920×1080 and at 640×360 with reduced motion emulated. The compact run explicitly verified that reduced motion disables stamp animation. Browser overrides were reset afterward.

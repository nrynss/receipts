# TU.4 chat feedback evidence

## Implemented

- `!receipt` sends `🧾 Receipt taken (@<login>): "<claim>"` only after `ReplyCaptured` with a nonnil receipt. Per-user and global reply limits bound chat volume; the rendered message is valid UTF-8 and no longer than 450 bytes. Existing command deduplication and capture results are unchanged. Capture errors, non-capture outcomes, and a missing sender remain silent.
- `!lore` retains its scoreboard prefix and annotates each claim with its outcome: `(open)`, `(broken)`, `(overturned)`, `(context changed)`, or `(inconclusive)`. Claim rendering keeps the full item tag within the 450-byte UTF-8 budget.
- Production chat composition now connects the verified router to the existing appeal service, ledger lore service, and authority-bound Twitch connection sender. This composition wiring is outside TU.4's nominal router/lore ownership, but without it neither chat replies nor the existing appeal/lore routes reached their services in the live EventSub path.

## Offline evidence

- `internal/twitch/chat` covers the success gate, silent capture outcomes, valid UTF-8, the byte ceiling, and throttling.
- `internal/lore` covers all outcome tags, retained scoreboard counts, UTF-8 validity, and the byte ceiling.
- `internal/twitch/eventsub` runs the synthetic acceptance sink through the real capture, appeal, pause/resume, lore, and chat-sender ports, asserting the receipt confirmation without Twitch access.

## Browser and live evidence

Mounted browser route verification was not run in this worktree. No Twitch chat send or paid provider call was made. Parent coordination is required to complete the mounted route checks and record that result here.

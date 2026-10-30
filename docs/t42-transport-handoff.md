# T4.2 EventSub and chat transport handoff

The new `internal/twitch/eventsub` client opens EventSub only beneath a supplied
TA.2 `auth.ChannelContext`. It has no runtime activation path. Fresh sockets
must send a valid Welcome before the channel-bound Helix adapter subscribes;
the subscription response must be `enabled`. The client applies the Welcome
subscription deadline, keeps each socket read bounded by Twitch's advertised
keepalive timeout, handles keepalives, and uses Twitch's reconnect URL without
modifying it. It waits for the replacement Welcome before closing the old
socket and does not create a second subscription during a migration. Ordinary
disconnects use a capped exponential retry and create a fresh subscription.
Authorization loss and chat-subscription revocation return to the supervisor
instead of retrying forever. Parent cancellation closes a pending socket read.

Frames are size limited and decoded into a strict notification envelope.
Notifications must identify the expected chat type/version, enabled status,
same broadcaster in the subscription condition and event, and the current
socket session. Their Twitch timestamp is required. The chat router validates
the actual broadcaster against the bound channel, maps broadcaster identity
and the moderator badge to product roles, bounds text and deduplicates Twitch
message IDs in a capped in-memory ring. It dispatches `!receipt` to the same
capture service used by replay, appeal and moderator commands to the same
appeal service, and `!lore` to the same lore service. Lore replies are limited
per user and channel, UTF-8 bounded, and go through the bound Helix chat adapter.
Application command errors do not reset the WebSocket; authorization loss does.

The transport packages are deliberately not mounted into production startup.
T4.4 must supply a persisted, verified live session and own runtime start/stop;
the caller can then register `Client.Run` with that runtime's context and hand it
the runtime authority. The adapter does not infer live status from Welcome,
chat traffic, or OAuth identity.

## Offline proof

Synthetic WebSocket fixtures are in `testdata/twitch/eventsub/`. In-memory
tests cover Welcome, subscription, keepalive, reconnect migration, revocation,
notification identity/timing, reconnect host checks, frame limits, role
mapping, channel binding, deduplication, command routing, lore limits, and
session cancellation. The replay path remains the authoritative product
service proof. Full repo gate and targeted uncached race checks are recorded
in the implementation handoff after final review.

## Bounded live acceptance

`TestLiveEventSubTransport` is opt-in and receive-only. It first validates the
existing broadcaster token and `user:read:chat` grant, opens one temporary
`channel.chat.message` subscription, makes no bot chat write and no clip/model
request, and lets Twitch disable the subscription when the WebSocket closes.
The test prints a safe `READY` line only after Welcome and subscription
success. The command window is 120 seconds from that line; setup is separately
bounded. Run it from the candidate worktree with the existing approved
environment already loaded (never print or paste its values):

```sh
GOCACHE=/tmp/receipts-go-cache go test -v -tags live ./internal/twitch/eventsub -run '^TestLiveEventSubTransport$' -count=1
```

After `READY`, the broadcaster types these exact commands in the authorized
chat:

1. `!receipt`
2. `!uphold`
3. `!receipts off`
4. `!receipts on`
5. `!lore`

The test creates a temporary synthetic runtime and SQLite ledger with an
authorized synthetic account, a seeded stamp and an open appeal. The `!receipt`
path receives a sanitized prerecorded broadcaster transcript segment so the
real capture service can prove the durable action; this does not claim live
transcription. `!uphold` must create exactly one vote. `!receipts off/on` must
change the temporary runtime's pause state. `!lore` must return a reconciled
two-receipt safe summary to a recording sink; the test never posts the reply to
Twitch. No real channel state, clip, persistent subscription, or production
runtime is created. This synthetic session intentionally does not prove T4.4
live-session authority or deployed end-to-end readiness.

The harness does not satisfy T4.2's real-channel acceptance until a live run
records `READY` and all five service outcomes without exposing chat text,
tokens, or viewer identities.

## Official provider references

- [Handling EventSub WebSocket events](https://dev.twitch.tv/docs/eventsub/handling-websocket-events/)
- [EventSub WebSocket message reference](https://dev.twitch.tv/docs/eventsub/websocket-reference/)
- [EventSub subscription types](https://dev.twitch.tv/docs/eventsub/eventsub-subscription-types/)
- [Sending and receiving chat messages](https://dev.twitch.tv/docs/chat/send-receive-messages/)

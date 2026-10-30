# Setup

## Hosted Twitch connection

Hosted mode requires the registered Twitch `TWITCH_CLIENT_ID` and
`TWITCH_CLIENT_SECRET`, a 32-byte `RECEIPTS_TOKEN_ENCRYPTION_KEY`, and an HTTPS
`RECEIPTS_PUBLIC_ORIGIN`. The callback URL registered with Twitch must match
the configured callback. Keep these values in the process environment or an
operator-managed secret store; do not commit them.

The owner signs in with Twitch and grants the channel's own user authorization.
The initial adapter needs only `user:read:chat`, `user:write:chat`, and
`clips:edit`: receive chat as the broadcaster, send chat as that broadcaster,
and create clips. Stream status lookup does not require another scope. The
adapter binds every Helix operation to the persisted Twitch subject and its
matching channel platform ID. A logged-in account by itself does not activate a
live runtime; activation requires trusted live-session authority.
The owner dashboard can use the bounded stream-status probe before runtime
activation; its online/offline observation is not proof that capture may start.

The service encrypts credentials at rest and shares TA.1's refresh and token
validation lifecycle. A single Helix 401 can trigger one refresh and retry. A
second 401 or revoked grant marks the connection for reconnect and stops its
channel runtime. Owner disconnect revokes the stored access token at Twitch,
revokes browser sessions, and stops that runtime. No token is returned in
health responses or included in logs.

## Local fixture mode

Use `RECEIPTS_MODE=fixture` for recorded adapters and the judge demo. Fixture
mode needs no Twitch credentials and does not open network connections.

## Bounded live check

The `live`-tagged `TestLiveConnectionCapability` is a read-only check. It makes
at most one Twitch token validation request and one `GET /helix/streams`
request, with a five-second client timeout and a 15-second overall deadline.
It has no chat, clip, EventSub subscription, model, or spend side effects. It
requires explicit `RECEIPTS_LIVE_CONNECTION=1` plus `TWITCH_CLIENT_ID`,
`TWITCH_ACCESS_TOKEN`, and `TWITCH_BROADCASTER_ID`. It is intentionally not
part of `tools/check.sh` and has not been run as part of this change.

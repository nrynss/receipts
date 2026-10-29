# Twitch integration probe

## Current documented contract

The selected probe channel is **nrynss**. Receipts uses a broadcaster-authorized user token for chat. Twitch's current chat authentication guide specifies `user:read:chat` to read chat EventSub messages and `user:write:chat` to send messages. Subscribe to EventSub `channel.chat.message` over WebSocket with both `broadcaster_user_id` and `user_id` in the condition. The probe requires a token validated as the nrynss broadcaster and carrying `user:read:chat`, `user:write:chat`, and `clips:edit`. The single-user client flow avoids the extra cloud-bot grants (`user:bot` and `channel:bot`) described for app-token bots.

Clip creation uses a broadcaster user token with `clips:edit`. `POST /helix/clips` returns a clip ID before processing completes; poll `GET /helix/clips?id=...`. Twitch documents treating the clip as failed if it has not appeared after 15 seconds. A clip embed requires an HTTPS page and the exact embedding hostname in its `parent` query parameter. Autoplay defaults off for clip embeds and is subject to visibility/size constraints and mobile user interaction. These are documentation-derived requirements, not live probe results.

## Live evidence

`internal/twitch/probe_live_test.go` implements one bounded live probe. Run it only after the streamer has said an original promise on nrynss and with the stream live; it listens for a viewer or the streamer to type exactly `!receipt probe`, sends one reply, creates one clip, and polls readiness for at most 15 seconds. Enable it explicitly with `RECEIPTS_LIVE_PROBE=1`; supply `TWITCH_CLIENT_ID`, `TWITCH_ACCESS_TOKEN`, and `TWITCH_BROADCASTER_ID`. Run `go test -tags live ./internal/twitch -run '^TestLiveChannelProbe$' -count=1`. It has a 90-second overall deadline, no model spend, one reply, one clip, and bounded HTTP calls. It never writes token or EventSub payload data. Optional `RECEIPTS_PROBE_OUTPUT` stores sanitized timing, clip ID and embed URL evidence with mode 0600. Playback is recorded as unverified; check the embed from the actual HTTPS OBS parent domain manually.

Live chat receive/reply, clip readiness and local iframe playback are now verified; measured evidence is summarized below. The owner has confirmed actual OBS playback at localhost; deployed-origin playback remains P4/P6 verification. Fixture and fake-transport results are kept separate from live evidence.

The live probe must record timestamps for chat command receipt, reply acceptance, clip creation response, readiness polls, and playback/autoplay observations. Redact access tokens and user-sensitive payloads. Stop if the probe exceeds the explicit cost/authorization budget.

## Sources

- [Twitch chat authentication](https://dev.twitch.tv/docs/chat/authenticating/)
- [Twitch OAuth scopes](https://dev.twitch.tv/docs/authentication/scopes/)
- [EventSub subscription types](https://dev.twitch.tv/docs/eventsub/eventsub-subscription-types/)
- [Handling EventSub WebSocket events](https://dev.twitch.tv/docs/eventsub/handling-websocket-events/)
- [Twitch clips API](https://dev.twitch.tv/docs/api/clips/)
- [Video and clips embedding](https://dev.twitch.tv/docs/embed/video-and-clips/)

The live probe uses `github.com/gorilla/websocket` for EventSub because Go's standard library does not provide a WebSocket client. The tested dependency handles the handshake and ping/pong behavior without a custom wire-protocol implementation.

## Registered probe app

Receipts by nrynss is registered under the nrynss account as a Confidential Chat Bot application. Public client ID: `f4qudtookd0irid7guj56yifn3cobf`. The registered local callback is `http://localhost:18081/auth/twitch/callback`. No client secret was generated. Initial registration was rejected for missing 2FA; after the owner enabled 2FA and signed in again, registration succeeded. Port 8081 is occupied by an existing Python service, so the receiver uses 18081 without changing that service.

The owner completed Twitch consent. The local receiver validated nrynss, the matching client ID and all three scopes, wrote the three probe variables to an ignored mode-0600 `.env`, and shut down. No token was printed or stored in the browser. The first bounded live test passed authorization but Helix Get Streams reported the channel offline; it stopped before subscribing, sending chat or creating a clip. A later live broadcast passed chat, clip and short audio probes; see the measured results below.

## One-shot local authorization helper

Once the registered public client ID is available, run `go run ./cmd/receipts-auth -client-id CLIENT_ID` from the repository root. The helper listens only on localhost:18081 for at most ten minutes and prints an authorization URL containing the public client ID and a random transaction state. It requests only `user:read:chat`, `user:write:chat` and `clips:edit`. The owner must review and authorize that access in Twitch.

This probe-only helper uses Twitch's implicit grant flow so no client secret needs to be generated or copied. The local callback clears the fragment containing the access token before posting it to the receiver. The receiver checks origin, host and transaction state, then validates the token with Twitch and requires the nrynss account, matching client ID and all requested scopes. It writes only the three probe variables to a new mode-0600 `.env` and refuses to overwrite an existing file. No token is printed, returned in the response or stored in browser storage. Use `-output PATH` to select another new private environment file. Production authorization-code flow, refresh/validation and local revocation/session handling are now implemented by TA.1; see [the auth handoff](../ta1-auth-handoff.md). P4 still owns live adapter integration and TA.4 hosted consent acceptance.

Load the private environment into the probe process locally; do not paste its contents into chat or a terminal command that prints values. The helper does not enable live tests or start a broadcast. See [Twitch OAuth flow documentation](https://dev.twitch.tv/docs/authentication/getting-tokens-oauth/) for the provider contract.

## Live capability result

The authenticated nrynss stream passed the self-triggered live probe: EventSub received the single `!receipt probe` command, Twitch accepted the reply, and one clip became ready in 4.898 seconds after its create response. The earlier manual-command run reached its 90-second deadline without the exact trigger; it sent no reply and created no clip. Set `RECEIPTS_PROBE_SELF_TRIGGER=1` to remove manual timing from the transport probe; that adds one chat write and raises its HTTP cap to 21. This proves the broadcaster-account command path, not a separate viewer account.

A temporary localhost iframe with `parent=localhost` and autoplay disabled played the created 30.021-second clip to completion. This establishes click-to-play at the local browser origin, not OBS execution, autoplay compatibility or the eventual deployed HTTPS origin. P4 and deployment still recheck those environments. Sanitized timings and playback observations are in `docs/evidence/p0-twitch.json`. The test broadcast showed third-party game-store artwork, so its media and clip references are excluded from repository/submission assets. Use original content for the final demo.

## OBS acceptance

The owner confirmed video and audio playback in an actual OBS Browser Source at `http://localhost:18082`, explicitly confirmed autoplay worked and also clicked Play to check playback. These are owner-reported results, not agent-observed OBS automation. `docs/evidence/p0-twitch.json` records provenance and each result. This closes bounded P0 playback feasibility; deployed HTTPS origin and final overlay behavior must be rechecked in P4/P6.

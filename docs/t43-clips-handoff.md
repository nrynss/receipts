# T4.3 clip jobs and playback handoff

T4.3 implementation candidate: `codex/t43-clips` from `85c69b8`.

## Implemented

- Clip jobs remain created in the same transaction as the transcript receipt. The worker resumes pending records after restart, leases each job to one active runtime generation, and persists the returned clip ID before readiness polling.
- Before the non-idempotent create POST, a durable intent and request timestamp are committed. If the process disappears after that marker without persisting a clip ID, recovery marks the job uncertain/failed instead of issuing another POST. Only a received 429 can clear the intent for a bounded retry. Jobs are claimed one at a time; HTTP calls have a shorter deadline than the lease.
- The job records the latest transcript evidence end and the delay to the clip create request for correlation. This timing is metadata, not proof that Twitch's resulting clip contains those words.
- Readiness uses channel-bound `GET /helix/clips?id=...`. A missing result is pending; a safe Twitch clip URL marks the job ready. A bounded deadline marks it unavailable. Permanent authorization/HTTP failures and ambiguous clip-creation outcomes are failed. The public claim and transcript evidence remain separate from these clip states.
- Job claims are atomic and expiring. Mutations verify account, channel, live session, receipt eligibility, runtime generation and lease token. Runtime stop/revocation and delayed old-generation responses cannot publish a result.
- Poll errors use bounded exponential retry. HTTP 429 honors a bounded `Retry-After`; its retry budget is capped. A failed or timed-out clip-creation POST is not retried blindly because Twitch may have created a clip before the response was lost.
- The overlay keeps the claim visible and says clip timing may not include every word. No transcript claim is inferred from the clip video.

The worker allows 60 seconds for readiness, following the POST section of Twitch's [API reference](https://dev.twitch.tv/docs/api/reference#post-clips). Twitch's separate [Clips API guide](https://dev.twitch.tv/docs/api/clips/) still says to assume failure after 15 seconds. This documentation conflict is recorded for acceptance; the longer bound avoids declaring failure before the reference's current POST contract. The embed still needs the exact `parent` domain and HTTPS as described in [Video and Clips embedding](https://dev.twitch.tv/docs/embed/video-and-clips/).

## Offline verification

Original bounded response fixtures live under `testdata/twitch/clips/`: processing, ready and a hostile URL. Connection tests make no sockets; worker tests cover recovery from a persisted ID, ambiguous POST failure, 429 retry bounds and stop races. Store migration 009 adds retry timing and lease fields to the existing durable job table.

Run the focused checks with:

```sh
go test ./internal/store ./internal/twitch/... ./internal/commands
```

The overlay fallback copy changed in `web/src/lib/overlay/Overlay.svelte`; the required project gate also checks overlay lint, types and tests.

## Live readiness and playback acceptance

`TestLiveClipReadiness` is prepared as a read-only check of one existing clip. It requires the owner to set `RECEIPTS_LIVE_CLIP_READINESS=1`, `RECEIPTS_LIVE_CLIP_ID`, and the existing `TWITCH_CLIENT_ID`, `TWITCH_ACCESS_TOKEN`, and `TWITCH_BROADCASTER_ID` environment values, then run:

```sh
go test -tags live ./internal/twitch/connection -run '^TestLiveClipReadiness$' -count=1
```

It validates the broadcaster/client and makes one bounded Helix lookup. It creates no clip, sends no chat, subscribes to no events and spends no model budget. The sanitized `docs/evidence/t41-adapter-live-write.json` confirms one clip ID was created but intentionally omits that ID; `docs/evidence/p0-twitch.json` and the owner report document successful prior localhost OBS playback, but neither gives this task a reusable clip ID. No read-only live check was run in this worktree.

For playback, open the running overlay as an OBS Browser Source at the origin that will actually host the overlay. The Twitch embed requires HTTPS and `parent` equal to that origin's hostname; the player has minimum viewport dimensions of 400×300, and autoplay can depend on size and visibility. Confirm the clip's video and audio in OBS, confirm clicking Play works when autoplay does not, and preserve the public claim fallback. To avoid feeding the clip back into listening, keep the Receipts audio capture source separate from the Browser Source; if OBS controls Browser Source audio, verify it is not routed to the captured mic/desktop input. Record the OBS version, exact overlay origin, audio routing and observed playback outcome in a new sanitized evidence file. The deployed origin is not available from this worktree, so deployed-origin and no-feedback acceptance remain open.

## Integration seam and limits

The parent runtime must call `Worker.Attach(authority)` after trusted activation with the same channel authority used by the connection adapter. Attachment is runtime-scoped; pending jobs for other/ended sessions are deliberately not picked up by a newer session. The parent owns the P4 index/task status and final review. The worker does not activate listening or authorize T4.4 behavior.

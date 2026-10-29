# T1.3 contract handoff

## HTTP surface

- `GET /api/overlay` returns the authoritative, privacy-filtered `OverlaySnapshot`.
- `GET /api/events` carries `snapshot`, `stamp`, and `status` SSE frames.
- `GET /api/review` and `GET /api/health` require the streamer control credential.
- `POST /api/control/pause`, `/void`, and `/review` require that same control credential. They return `501` until an application action port is connected; authentication alone never reports success.
- `POST /api/control/decision` accepts the typed streamer intents `admit`, `appeal`, or `context_changed`; implementations remain application-owned.

The overlay and control credentials are sent in `Authorization: Bearer …` headers. `overlayURL` contains no credential. Browser source clients should use Chaaya `createEventStream` with the read-only header, then fetch the snapshot with the same header.

## Replay and recovery

Keel `stream.Broker` owns ordered SSE IDs and a 128-frame per-topic replay ring. These IDs live only as long as the process and are not the ledger cursor. The API emits an initial `id: 0` `reset` frame from the first SSE flush, after `ServeTopic` has atomically registered its subscription/replay position. Chaaya must include `reset` in its event filter so its parser resets a previous process's larger numeric cursor before new Keel IDs arrive. An unknown or expired Last-Event-ID is handled by Keel as a fresh subscription.

Chaaya `catchUp` fetches `/api/overlay` on each initial open and reconnect; reset frames also request a rebase, deduplicated by one in-flight fetch. Hold events until that fetch completes, then replace client state from the snapshot and apply only events whose epoch matches and whose sequence is exactly the next sequence. Ignore replayed events at or below the snapshot sequence. On an epoch change or sequence gap, fetch and replace from the authoritative snapshot again. The pending rebase buffer is capped at 128 events. Stamp events are transient stream events; `current_stamp` in a snapshot is state only and must not be animated as a newly landed stamp. Keel registers the subscription before the response flush, so publish during that boundary is buffered. The API does not claim that separate HTTP requests make an atomic snapshot plus subscription; sequence checks and snapshot rebase provide recovery.

`events.OverlaySnapshot` and `OverlayEvent` intentionally expose no transcript text, speaker attribution, evidence text or internal review details. The `Reader` implementation must exclude void, sensitive and otherwise non-public receipts before constructing `PublicReceipt`. This layer defines contracts, not ledger policy or application actions.

## Integration

Startup should create one `events.Publisher` for the process and pass it into `api.Config.Events`. Every overlay-visible state mutation and event must use `Publisher.Commit`; `Publisher.Snapshot` holds the same mutex around the authoritative read and assigns the matching epoch/version. The read model implements `api.Reader` over the T1.2 ledger and process health; a later application service implements `api.Controls` before control POSTs can succeed. Provider adapters translate incoming data to `ports.TranscriptSegment`, `ChatCommand`, `ClipResult` and `LiveSessionChange`, then pass them to application sinks. Chat role must come from trusted EventSub metadata. Clip pending, ready, unavailable and failed outcomes remain distinct. Transcript attribution is `broadcaster` or `uncertain`. Adapters do not decide whether a transcript line is a receipt, whether a stamp is valid, or whether the stream is live enough to listen.

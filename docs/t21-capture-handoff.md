# T2.1 receipt capture handoff

`commands.Service` is the application decision path for `!receipt`. Callers pass an explicit trusted `commands.Authority` containing the active channel and session. Incoming chat channel/session fields are ignored; the broadcaster's trusted chat adapter supplies the Twitch user ID and login used for credit.

The extractor receives only broadcaster-attributed transcript segments from the configured lookback window. Its recorded/production response must cite one or more source segment IDs. Capture rejects citations outside that bounded set; the store then rechecks live session ownership, channel ownership and exact source text/offset/attribution in the same transaction as receipt and evidence insertion. Sensitive classification is required and fails closed. Missing speech, extraction failure, sensitivity and duplicate/throttled commands have separate result codes for safe adapter replies.

Migration `004_capture.sql` persists command IDs per channel, user/channel throttles, viewer credit and independent clip jobs. A command is claimed before transcript lookup/extraction, so redelivery cannot multiply work; an interrupted `processing` command stays deduplicated. The receipt transaction creates a pending clip job separately from the text receipt. Clip dispatch or failure cannot roll back the captured claim. Clip-job result handling remains a downstream adapter responsibility.

`testdata/replay/t21-capture.json` runs the same service through `replay.Fixture.Run`, using the recorded extraction response in the service test. No provider or model is called by the offline fixture.

The service is not yet mounted in the production chat/runtime composition. TA.2 must pass its validated channel/session authority to this service and keep EventSub identity metadata trusted; P5 supplies the live extractor and sensitive-topic classifier. The initial throttle policy is one request per viewer per channel in 30 seconds and at most three channel requests in any five-second window.

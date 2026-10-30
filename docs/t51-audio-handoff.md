# T5.1 audio work in progress

This branch contains unconnected components, not a completed T5.1. Production
session authority does not attach these workers and still reports audio capture
inactive. No live provider call has been made.

The file-transcription adapter uses OpenAI `gpt-transcribe`, WAV input and
non-streaming final JSON. [OpenAI's speech-to-text guide](https://developers.openai.com/api/docs/guides/speech-to-text) recommends that model for bounded files and describes timestamp and speaker-label support on deprecated models. The [pricing page](https://developers.openai.com/api/docs/pricing) lists $0.0045 per minute. Set `RECEIPTS_TRANSCRIPTION_MODEL=gpt-transcribe` for live mode. A one-minute conservative reservation is charged for each attempted call, including an ambiguous failure or cancellation; this is an estimate, not measured provider usage.

The source seam bounds temporary files and queue size and deletes the capture directory on exit. Its first decoded PCM observation anchors an approximate ingest clock. HLS delay is unknown; neither the chunk interval nor the transcript is an exact broadcast, clip, or word timestamp. This basis is persisted with live transcript rows and copied to receipt evidence. The worker validates actual PCM, requires a session clock and rejects future, overlapping and repeated chunks. It persists a pre-call admission so a restart cannot replay the same audio range. Mixed audio defaults to uncertain attribution; the caller must not set the explicit broadcaster flag without a trusted, per-session owner attestation.

Before production use, complete and test the Authority attachment, lifecycle and source-failure shutdown, truthful capture status, owner/session attestation and reset, source clock mapping and its uncertainty, and microphone dock behavior. Review and extend the currently partial offline coverage, including real SQLite and in-memory HTTP integrated end-to-end. Prepare a single original-content live case with a $0.02 hard ceiling and coordinate it before any paid request. T5.1 acceptance requires aligned evidence and a verified offline or source-failure stop; no such evidence exists yet.

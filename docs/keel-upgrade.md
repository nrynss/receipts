# Keel v0.7.3 → v0.9.0

Receipts now pins v0.9.0 in go.mod and AGENTS.md. Go module @latest resolved to v0.9.0; its published tag resolves to a4cd94a8307edc10394f5ed854ef518da1ac5dd6. Comparison used the published module source for both versions, not a stale checkout. No Go version or transitive dependency changes are required by this upgrade.

## Changes relevant to Receipts

- SQLite writer transactions now begin IMMEDIATE, acquiring the write lock before reads and waiting under the busy timeout instead of failing a later lock upgrade. New DB.BeginWrite(ctx) preserves cancellation/deadline identity alongside driver errors; Keel stores and migrations adopt it. Existing Writer().BeginTx calls still compile and receive IMMEDIATE behavior; prefer BeginWrite for new transaction code.
- Cost adds ContextAccount, Hold and NewContextMeter so durable SQLite budgets can use the same reserve/work/settle/release flow as memory budgets. Settlement, release and charge recording survive caller cancellation. The old Account/NewMeter interface remains compatible but Account is deprecated. Inspect this before implementing TA.2 per-channel/global spend accounting.
- ffmpeg adds RunTo and ProbeTo for caller-owned stdout parsing. Existing capture calls remain compatible.
- New probe, fit, mix and revision packages cover typed media inspection/audio extraction, bounded tempo fitting, timeline mixing and document history. They do not require adoption for the current receipt loop.
- throttle.Note now distinguishes transient exhaustion from throttling and reports the corresponding attempt count. EDL loudness implementation is shared with mix; cache/provider-task tests were expanded.
- identity/sqlitestore switches to BeginWrite. Identity/session/external-provider facilities already existed in v0.7.3: TA.1 inspected these; its fit decision and app-owned session/token boundary are documented in [the auth handoff](ta1-auth-handoff.md).
- Receipts-imported config, config/source, id, stream and wire source are unchanged. go.mod is identical between Keel tags.

## Verification

The complete offline gate and uncached race checks for store, API, events, settings, audio and startup passed on the upgraded Receipts tree. Replay and TA.1 integrated this dependency and passed combined checks in `ed2c7d6`. This comparison does not claim live Twitch or ffmpeg deployment acceptance.

## Source

Published module source: [Keel comparison](https://github.com/nrynss/keel/compare/v0.7.3...v0.9.0). Local sources were downloaded and checksum-verified with go mod download.

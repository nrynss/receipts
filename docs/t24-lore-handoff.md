# T2.4 lore handoff

The lore service reads current eligible receipts directly from the ledger on each request. Its score counts each valid, non-sensitive, unexpired receipt once: admitted and upheld are broken; undecided, overturned, context changed and inconclusive are separate categories. The response is bounded to 450 UTF-8 bytes. The chat handler requires a matching channel and a bare `!lore`; verified chat delivery remains P4.

The offline acceptance test drives `testdata/replay/t21-capture.json` and `testdata/replay/t24-loop.json` through the actual capture, detection, appeal and lore services with deterministic fixture time and IDs. A recorded judge response and in-memory extractor and clip transport make no provider calls. A failed clip remains recorded, the database is closed and reopened with an overdue appeal, and recovery emits the final verdict. Two fresh runs compare all relevant persisted rows, including the entire verdict sequence, and assert the complete expected stamp and status event payloads across the restart. Sensitive and void filtering are checked after state changes. Publisher epochs are deliberately fixed in the fixture because production runtime epochs are random and distinguish restarts.

Trusted live chat delivery and runtime activation remain P4. The full offline gate and uncached races for lore, store, commands, detect, appeal and replay passed.

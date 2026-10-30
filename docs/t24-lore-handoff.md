# T2.4 lore handoff

The lore service reads current eligible receipts directly from the ledger on each request. Its score counts each valid, non-sensitive, unexpired receipt once: admitted and upheld are broken; undecided, overturned, context changed and inconclusive are separate categories. The response is bounded to 450 UTF-8 bytes. The chat handler requires a matching channel and a bare `!lore`; verified chat delivery remains P4.

An offline test exercises real capture, detection, appeal, restart recovery and lore against two fresh ledgers, with deterministic IDs and a failed clip. It compares persisted capture, detection, appeal, vote and verdict tables and checks sensitive/void suppression after changes. The extractor and judge are in-memory recorded responses; no provider is called. The full offline gate and uncached races for lore, store, commands, detect, appeal and replay passed.

This is a candidate, not complete T2.4 acceptance: the scenario is coded in the test rather than the original recorded fixture, and it compares persisted table rows and event counts rather than complete emitted event payload sequences. The scenario must be extended to the original demo fixture and exact event sequence assertions before closing T2.4. Live chat activation remains P4.

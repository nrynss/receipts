# TA.4 recorded demo handoff

Status: **in progress; hosted acceptance pending T6.1 deployment and fresh two-owner rehearsal.** This worktree does not establish hosted readiness.

## Existing offline acceptance

`internal/lore/lore_test.go` contains the strongest current end-to-end acceptance. It composes the recorded capture fixture with `commands.Service`, the recorded detection judge with `detect.Service`, `appeal.Service`, and `lore.Service`, then checks the durable upheld verdict and rendered lore. The test also observes overlay events and appeal recovery after reopening SQLite. `cmd/receipts-acceptance` is a separate TA.3 synthetic owner/OBS harness and is not an anonymous demo.

## Integration constraint

The T2.4 replay fixtures carry deterministic channel/session IDs (`...01` and `...02`), while `replay.OpenLedger` creates those fixed fixture identities. The capture command intentionally ignores untrusted channel/session selectors, but the transcript and detection services validate session authority against the ledger. A public demo must therefore remap fixture-owned transcript and chat IDs to the visitor's server-created channel/session before dispatch, or use equivalent server-owned fixture inputs. It must never accept those IDs from the browser or share a fixture ledger across visitors.

## Hosted acceptance boundary

TA.4 remains incomplete until the candidate is deployed after T6.1 and passes HTTPS checks, lifecycle/deletion acceptance, a fresh Twitch-account rehearsal, and two-owner isolation rehearsal. Offline tests can establish recorded-service behavior, visitor isolation and zero provider calls; they do not establish hosted readiness.

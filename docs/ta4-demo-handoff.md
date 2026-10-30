# TA.4 recorded demo handoff

Status: **anonymous recorded demo implemented; hosted acceptance remains pending T6.1 deployment and fresh two-owner rehearsal.** This worktree does not establish hosted readiness.

## Implemented demo

`/demo/` is a prerendered Svelte route with replay/reset controls. The Go API runs the original recorded capture, contradiction, vote, and lore inputs through `commands.Service`, `detect.Service`, `appeal.Service`, and `lore.Service`. It displays the resulting claim and upheld lore; recorded clip failure is explicit, and the demo makes no provider calls.

Each visitor gets a separate temporary SQLite ledger and replay clock. A cryptographically random 256-bit HttpOnly, Secure, SameSite=Strict cookie binds to the server-side visitor entry by its SHA-256 digest. Fixture IDs are confined to that visitor database and are not accepted from request bodies or selectors. Active visitors are capped at 64, sessions expire after 20 minutes, each visitor is limited to 30 API operations, replay has a five-second deadline, and reset, expiry, failed replay, and shutdown close and remove the visitor database.

Principal integration revision: `431ca1e`. Browser replay/reset and keyboard replay pass; the recorded clip failure, upheld verdict and lore are visible.

The full offline gate passes with `./tools/check.sh`; visitor concurrency also passes `GOCACHE=/tmp/receipts-go-cache go test -race ./internal/demo ./cmd/receipts`.

## Existing offline acceptance

`internal/lore/lore_test.go` contains the strongest current end-to-end acceptance. It composes the recorded capture fixture with `commands.Service`, the recorded detection judge with `detect.Service`, `appeal.Service`, and `lore.Service`, then checks the durable upheld verdict and rendered lore. The test also observes overlay events and appeal recovery after reopening SQLite. `cmd/receipts-acceptance` is a separate TA.3 synthetic owner/OBS harness and is not an anonymous demo.

## Integration constraint

The T2.4 replay fixtures carry deterministic channel/session IDs (`...01` and `...02`), while `replay.OpenLedger` creates those fixed fixture identities. The capture command intentionally ignores untrusted channel/session selectors, but the transcript and detection services validate session authority against the ledger. The implemented demo remaps fixture-owned transcript and chat IDs to its visitor ledger authority before dispatch. It must never accept those IDs from the browser or share a fixture ledger across visitors.

## Hosted acceptance boundary

TA.4 remains incomplete until the candidate is deployed after T6.1 and passes HTTPS checks, lifecycle/deletion acceptance, a fresh Twitch-account rehearsal, and two-owner isolation rehearsal. Offline tests can establish recorded-service behavior, visitor isolation and zero provider calls; they do not establish hosted readiness.

Principal browser review verified replay, reset and keyboard replay on the built fixture-mode server. The page showed the actual upheld verdict, failed recorded clip and reconciled lore. No saved browser screenshot artifact is claimed. Abrupt process termination can leave temporary visitor files; T6.1 must define ephemeral-volume/startup cleanup before hosting.

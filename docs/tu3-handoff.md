# TU.3 Handoff

## Implemented

- `/control` now asks for sole-broadcaster attestation at each session start and clears it before attempting the start request.
- Stream status comes from the live `/account-api/status` observation and health confirmation. Failed or unknown status reads render `Unknown`; persisted session state alone is not shown as live confirmation.
- Quiet review cards separate **THE PROMISE** from **HEARD NOW**. The latter is loaded from the detection evidence text, scoped through the existing owner and channel checks, with a generic fallback only for empty text.
- The control dock lists active channel receipts with verdict, viewer credit, and one-click void actions. The manual receipt ID form remains. Active appeals show a countdown and vote split.
- `/account` has broadcast status, OBS pairing, and account/danger cards. The OBS URL copy action contains no pairing credential; paired devices show a short ID and timestamp. Sign out is visually separated from account danger actions.
- The unpaired `/obs` screen shows the brand mark and `RECEIPTS · OBS OVERLAY` label while retaining its transparent browser-source canvas.

## Verification

- `GOCACHE=/tmp/receipts-go-cache ./tools/check.sh` passed, including Go vet/tests and overlay lint, type check, tests, and production build.
- Mounted browser review was unavailable in this session; the connected browser inventory had no open task tabs. Parent can complete visual acceptance in the mounted browser.
- No live service, paid API, or account action was used.

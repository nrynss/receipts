# Bounded hosted rehearsal

Status: prepared, not run. This requires explicit live-test approval before
capture, model calls, chat replies or clip creation. The clip-worker wiring fix
must be deployed and healthy first. Automatic stamps stay disabled.

## Proposed scope and limits

One nrynss stream, with nrynproton acting as viewer and retaining its separate
Receipts owner session. One original spoken promise, one viewer `!receipt`, one
automatic receipt confirmation in nrynss chat, one Twitch clip, and one spoken
contradiction. Inspect receipt-stage usage and results before continuing to
owner review, appeal, one vote and lore. Stop on errors, ambiguous usage or
unexpected provider activity; no blind retries or larger run are approved.

Proposed total ceiling: $0.25, enforced through equal global, channel and
session durable limits after confirming the hosted ledger has no prior model
charges. No session starts until approval. Record measured settled charges and
unresolved reservations separately. A ceiling is not a predicted cost.

## Operator preparation

- Confirm no active live session, the reviewed image, healthy HTTPS, both owner
  identities and the existing nrynss paired source.
- Configure and verify the explicit spend limits before starting.
- Keep clip audio out of the broadcast listening mix. Use headphones and muted
  clip output, or monitor-only routing that does not feed the broadcast. If the
  routing cannot be established, use the truthful transcript fallback and do
  not claim feedback-free clip audio playback.
- Keep nrynproton's Receipts control page open in its separate browser profile.
  It must not gain nrynss's claim, review queue or control actions.

## Human sequence

1. Start a short nrynss stream with the owner as the only speaker. Start the
   Receipts session with sole-speaker confirmation and wait for Listening.
2. Say: "I promise I will always pick the blue card." As nrynproton in nrynss
   chat, send `!receipt` once when capture is ready. Check the receipt and the
   bounded clip job. Inspect usage before the next step.
3. Say: "I am picking the red card now." Wait for the actual model result.
   If it abstains or suppresses the claim, record that outcome rather than
   forcing an approval or repeating paid attempts.
4. If a review appears, approve it as nrynss. Verify the paired overlay's saved
   claim and clip or labelled fallback. Open Appeal, vote once with `!uphold`,
   and check `!lore` after the outcome. Exercise pause and void only after
   recording the unmodified result.
5. Refresh nrynproton's own Receipts control page and verify it never displays
   the nrynss receipt/review. Compare the owner-scoped ledger and API behavior
   without sharing cookies or credentials between browser profiles.
6. Stop listening and end the stream. Verify capture exits, late work cannot
   publish, and final ledger, latency, spend and any manual interventions are
   recorded. Keep fixture-rendering evidence separate from this real run.

This is the planned overlap between remaining TA.4 populated-channel isolation
and T6.2. It does not claim either task complete.

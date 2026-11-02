# Capture and owner review fixes

## Split promises

The capture worker now transcribes overlapping windows of original PCM audio. Adjacent nominal five-second chunks provide one contextual window.

The ledger records both raw chunk ranges before transcription. Windows cannot bridge gaps or cross session authority.

Exact returned text remains authoritative. The worker suppresses identical text within overlapping time ranges.

Offline fixtures cover a promise split across two chunks. They verify sample order, evidence bounds, replay protection and downstream review creation.

This verifies composition and provenance. It does not establish real ASR accuracy for the recorded failed take.

## Control refresh

The dock now refreshes its full private read model every three seconds. It loads new reviews without a manual refresh.

Cancellation prevents background reads from replacing an action result. Polling preserves the owner's typed reason.

The mounted delayed-review fixture showed an initially empty queue followed by a new review. The typed reason survived polling.

## Historical review approval

The queue previously returned open reviews across sessions. It sorted historical reviews before current reviews.

Approval correctly required the current live session. Those rules made historical queue items impossible to approve.

A read-only production audit found seven eligible historical open reviews before six reviews from the latest session.

The failed recording clicked six items. It did not retain clicked review IDs or refusal responses.

The regression reproduced historical items appearing before current reviews. The queue now restricts results to the authenticated live session.

Historical rows remain durable. The fix preserves approval authority and source validation.

The authenticated HTTP regression verifies:

- Historical reviews stay outside the actionable queue.
- Current review approval returns 204 and saves one action.
- Approval emits one stamp event.
- Retrying the same approval emits no duplicate stamp.
- Direct historical approval remains refused.
- Missing CSRF remains refused without a stamp.

Refusal logs report fixed stage names and HTTP status codes. They exclude evidence, identifiers, cookies and raw database errors.

The full offline gate and targeted race checks passed. Hosted replay and actual OBS observation remain separate acceptance checks.

---
title: Evidence and evaluation
description: Separate verified observations from unresolved behavior and historical failures.
---

## Verified observations

| Stage | Observation | Boundary |
| --- | --- | --- |
| Capture | A viewer command saved a promise, created a ready clip, and received confirmation. | The promise used a disclosed synthetic stock voice. |
| Detection | An owner-spoken later line created private review after a matching repair. | The owner approved one review before a stamp. |
| Appeal | One `!uphold` vote closed the appeal as upheld. | One vote does not establish broader chat participation. |
| Overlay | The paired OBS source displayed “Upheld by Chat”. | Clip playback and fallback remain unconfirmed. |
| Controls | Pause, resume, and durable void worked in the staged run. | Latest hosted restart recovery remains unconfirmed. |

The [live acceptance report](https://github.com/nrynss/receipts/blob/main/docs/evidence/t62-live-acceptance.md) records methods, outcomes, and remaining checks. The [TA.4 report](https://github.com/nrynss/receipts/blob/main/docs/evidence/ta4-hosted-smoke.md) documents hosted isolation and pairing checks.

Production combines adjacent raw chunks of about five seconds each into overlapping ASR windows. A window spans nominally ten seconds and has a twelve-second hard limit. Raw claims stay disjoint, and durable `transcription_windows` rows reference both audio ranges. Exact full ASR text and window bounds remain evidence. Receipts skips identical text from shared overlap. It rejects gaps, stale ownership, and duplicate raw submissions.

Longer promises or ASR errors can still produce abstentions. These fixes passed offline checks and a delayed dock fixture. Paid live verification remains pending.

An offline regression traced an approval failure to ended-session reviews appearing before current-session reviews. Approval correctly rejected those old reviews.

The queue now selects reviews from the current live session. Authenticated HTTP regressions verify approval returns 204, creates one stamp, and creates no duplicate on retry. Tests also reject ended-session reviews and requests without CSRF protection.

The staged approval remains evidence for that run only. Available logs cannot tie the historical failure to a review ID because the response and click record omitted IDs. The fixes are deployed. Fresh hosted verification remains pending.

## Evaluation limits

The reviewed development comparison found a false contradiction at confidence 1.0. Sensitivity checks suppressed classification on some reviewed pairs. The data does not establish prospective accuracy or sensitivity error rates.

Every contradiction remains review-only. The test corpus and threshold review do not establish production reliability. Read the [model migration and evaluation record](https://github.com/nrynss/receipts/blob/main/docs/model-migration.md).

## Historical failures stay historical

An earlier live run failed to reach contradiction review. A later code repair passed offline regression checks, then a staged live rerun produced private reviews. We describe each result in its own time and context.

No empty review screen counts as proof of a successful approval. A visible verdict alone does not count as proof of playback or restart recovery.

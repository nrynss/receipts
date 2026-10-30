# Evaluation contract

The initial eligible claim is a first-person, explicit future commitment made by the streamer: a stated intention that contains a clear action and a commitment marker such as “I will”, “I promise”, or “I’m never going to”. Predictions, preferences, dares, bets, quotations, third-party speech, and ambiguous plans are not eligible until separately evaluated.

The labelled corpus is `testdata/eval/pairs.jsonl`. IDs are stable; `split` is fixed as `dev` or `heldout`, `label` is one of `contradiction`, `no_contradiction`, `review`, or `suppress`, and `expected` is one of `stamp`, `quiet`, or `review`. All examples are original text written for this project. No recorded streamer audio, third-party material, or model output is included.

The current Judge returns discrete verdicts and no confidence score, so there is no numeric confidence threshold to tune. Freeze the development policy before collecting held-out results. Do not change held-out examples or labels in response to their results. If a separately evaluated policy later introduces a threshold, tune it on `dev` only. After independent label review, report the frozen policy and raw counts alongside these rates:

- Precision = correct automatic contradiction stamps / all automatic contradiction stamps.
- Recall = correctly stamped labelled contradictions / all labelled contradictions eligible for automatic stamping.
- False-stamp rate = incorrect automatic contradiction stamps / all labelled `no_contradiction`, `review`, and `suppress` later lines.
- Abstention rate = eligible pairs sent to quiet review or suppressed / all eligible pairs.

Review and suppression labels never count as positive automatic stamps. Sensitive-topic examples and unattributed or quoted speech expect suppression or review. The transcript fixture is a synthetic spoken script, not an audio recording.

## Initial label review

Before any model tuning, three development pairs (P0-004, P0-015 and P0-024) were changed to review: a later statement does not prove a future commitment was broken when an intervening completion or missing deadline could explain it. Four additional development pairs cover health, family and uncertain speaker attribution. The corpus now contains 44 pairs: 34 development and 10 held-out. The held-out partition and its labels are unchanged. No model results informed this review.

## Recorded corpus runs and review status

The frozen `review-only-v1` production policy routes contradictions and unsure judgments to private review; it does not issue automatic stamps. Its [freeze manifest](evidence/t54-policy-freeze.json) binds the dataset, source versions and the completed development report before held-out collection. The pinned Judge is `gpt-4o-mini-2024-07-18`, and the embedding model is `text-embedding-3-small`. Each recording binds the dataset and source hashes, exact request-body digests, sanitized adapter responses, and measured usage.

| Partition | Cases | Provider calls | Measured spend | Sum of case latency | Source rejections | Automatic stamps | Errors / pending |
|---|---:|---:|---:|---:|---:|---:|---:|
| Development | 34 | 758 | $0.01842507 | 691.070 s | 3 | 0 | 0 / 0 |
| Held-out | 10 | 210 | $0.00522862 | 189.183 s | 2 | 0 | 0 / 0 |

The successful corpus runs cost $0.02365369 in measured provider usage. These are sums of per-case elapsed times, not a latency percentile or a real-stream performance claim. The sessions construct artificial chronology and broadcaster attestation, and use three deterministic distractor claims from the same partition; they do not establish real-world attribution or generalization. All 968 recorded calls had measured usage. Offline replay reproduces both reports byte-for-byte.

There is no independent human review assertion for the Codex-authored labels. Accordingly, neither report publishes label-derived precision, recall, false-stamp rate, abstention rate, or retrieval-miss counts. Zero automatic stamps is a property of the review-only policy, not a precision estimate. No numerical threshold has been selected and automatic live stamping remains disabled. The [44-case review worksheet](t54-label-review.md) and deliberately blank [assertion template](t54-label-review-assertion.example.json) are ready for independent review. A changed dataset requires a new frozen evaluation protocol; the program checks an assertion's hash but cannot authenticate its reviewer.

The earlier interrupted development run is separate from these successful corpus runs: its preserved local ledgers show 689 settled calls totaling $0.01652705 and one unresolved attempt. That attempt was reserved for at most $0.025; its actual provider charge remains unknown and requires reconciliation. Earlier single-session runs and their costs are documented in the [T5.4 handoff](t54-evaluation-handoff.md).

To reproduce the shipped routing reports offline from the repository root, with the source revision matching the recordings:

```sh
GOCACHE=/tmp/receipts-go-cache go run ./cmd/receipts-eval-sessions -dataset testdata/eval/pairs.jsonl -recording docs/evidence/t54-dev-recording.json > /tmp/t54-dev-report.json
cmp /tmp/t54-dev-report.json docs/evidence/t54-dev-report.json
GOCACHE=/tmp/receipts-go-cache go run ./cmd/receipts-eval-sessions -dataset testdata/eval/pairs.jsonl -recording docs/evidence/t54-heldout-recording.json -freeze docs/evidence/t54-policy-freeze.json > /tmp/t54-heldout-report.json
cmp /tmp/t54-heldout-report.json docs/evidence/t54-heldout-report.json
```

The [development recording](evidence/t54-dev-recording.json), [development report](evidence/t54-dev-report.json), [held-out recording](evidence/t54-heldout-recording.json), and [held-out report](evidence/t54-heldout-report.json) are sanitized, byte-for-byte copies of the recorded artifacts. They contain synthetic source material and local ledger paths, no provider response bodies or credentials.

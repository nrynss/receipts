# T5.4 Decisions development comparison

## Decision

Use **0.6 minimum choice confidence and 0.6 minimum selected-choice probability** only as the candidate operating point for a parent-reviewed prospective follow-up. Do not freeze it for held-out scoring yet and do not enable automatic stamps. It leaves one known false stamp (P0-021) and rejects most otherwise eligible cases. The stronger conservative conclusion from this post-collection run is that no non-zero-stamp threshold in the tested grid eliminates all observed false stamps. Keep the current review-only behavior until the semantic failure modes are addressed and a prospective corpus confirms the result.

The 0.6/0.6 point is preferred over the permissive 0/0 point because it reduces false stamps from three to one and the false-stamp rate from 3/22 (13.6%) to 1/22 (4.5%). In exchange, correct stamps fall from five to two, recall from 5/12 (41.7%) to 2/12 (16.7%), and abstentions rise from 21/34 (61.8%) to 29/34 (85.3%). Precision changes only from 5/8 (62.5%) to 2/3 (66.7%), a three-stamp denominator too small to support a reliability claim. At 0.9/0.9 the system emits no stamps and abstains on all 34 cases; that avoids observed false stamps only by providing no positive automation.

The 49 grid points collapse to these distinct outcome groups:

| Threshold combinations | Stamps | Correct / false | Precision | Recall | False-stamp rate | Abstentions |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 2 | 8 | 5 / 3 | 62.5% | 41.7% | 13.6% | 21 (61.8%) |
| 1 | 4 | 2 / 2 | 50.0% | 16.7% | 9.1% | 26 (76.5%) |
| 3 | 3 | 2 / 1 | 66.7% | 16.7% | 4.5% | 28 (82.4%) |
| 6 | 3 | 2 / 1 | 66.7% | 16.7% | 4.5% | 29 (85.3%) |
| 8 | 1 | 0 / 1 | 0% | 0% | 4.5% | 31 (91.2%) |
| 5 | 1 | 0 / 1 | 0% | 0% | 4.5% | 33 (97.1%) |
| 24 | 0 | 0 / 0 | undefined | 0% | 0% | 34 (100%) |

Each nonzero-stamp point includes P0-021 among its false stamps. There is no tested operating point with one or more correct stamps and zero false stamps.

## False-stamp review

At 0/0, the three false stamps are:

| Case | Claim and later text | Owner-reviewed rationale | Luna judge output |
| --- | --- | --- | --- |
| P0-015 | “I will sell the old lantern.” / “I kept the old lantern.” | Keeping it now does not establish a missed deadline. | Contradicts; confidence 0.66, choice probability 0.77. |
| P0-021 | “I will never use the ice spell.” / “I used the ice spell once to escape a trap.” | Owner correction: event timing is not established; the spell might have been used before the promise. | Contradicts; confidence 1.0, choice probability 1.0. |
| P0-024 | “I promise I will play another round.” / “I am done playing for tonight.” | A round may already have been played; timing is unclear. | Contradicts; confidence 0.38, choice probability 0.59. |

At 0.6/0.6, P0-015 and P0-024 drop out, but P0-021 remains a false stamp at the maximum reported confidence and probability. Raising the score thresholds therefore does not address the most important error: the model infers contradiction without establishing event order. The owner-reviewed explanations are recorded in [`t54-label-review.md`](../t54-label-review.md); the full selected-point predictions are in [`t54-decisions-dev-threshold-06-report.json`](t54-decisions-dev-threshold-06-report.json).

## Sensitivity gate and measured cost

All 34 pairs received four sensitivity classifications: both models on the claim and later text. GPT-6 Luna returned `not_sensitive` for 39/68 texts, `unsure` for 24/68, and `sensitive` for 5/68. Under fail-closed behavior, at least one Luna `unsure` or `sensitive` result suppressed judge calls on 19/34 pairs. GPT-4o mini flagged at least one text on 3/34 pairs, all of which were already suppressed by Luna. Fifteen pairs reached both judges. These are gate rates only: the corpus has no independent per-text sensitivity labels, so this comparison cannot estimate sensitivity false positives or false negatives. The high Luna abstention rate is currently a greater deployment concern than the price of the Decisions endpoint.

| Component | Calls | Measured spend |
| --- | ---: | ---: |
| GPT-4o mini sensitivity | 68 | $0.00108375 |
| GPT-6 Luna Decisions sensitivity | 68 | $0.00122570 |
| GPT-4o mini judge | 15 | $0.00068640 |
| GPT-6 Luna Decisions judge | 15 | $0.00037860 |
| **Total** | **166** | **$0.00337445** |

End-to-end case latency totals 95.994 seconds across 34 cases: mean 2.823 seconds, median 2.714 seconds, p95 4.149 seconds, and maximum 6.016 seconds. Per-adapter latency was not recorded, so these measurements do not support a direct Decisions-vs-Chat-Completion latency comparison. The completed checkpoint contains measured usage for every attempt, no pending events, and no provider response bodies.

## Evidence provenance and limits

The exact sanitized checkpoint, original full sweep, and selected 0.6/0.6 report are included here. Their SHA-256 hashes are:

| Artifact | SHA-256 |
| --- | --- |
| [`t54-decisions-dev-approved-checkpoint.json`](t54-decisions-dev-approved-checkpoint.json) | `079007fe727b3eb60eb5f64dde337da687b32a27db343525a8231ff579aed118` |
| [`t54-decisions-dev-sweep.json`](t54-decisions-dev-sweep.json) | `14feba707b0be3e64691097b2311bd5622bf9d35ed83b8b8e3b0cd32e40dbcb0` |
| [`t54-decisions-dev-threshold-06-report.json`](t54-decisions-dev-threshold-06-report.json) | `ce8e6f5eb20cebad8785f8ef2aa9e0cf9eda5faf2438c540e92f3d667e8ecd1b` |

The owner-reviewed labels were applied after the original 44-item synthetic corpus and its model recordings were collected. Thus this 34-case dev sweep is useful for examining errors and selecting a candidate operating point, but it is not an independent prospective evaluation. Do not infer a held-out claim from these results. Any later held-out run requires parent review of this report, approval of the threshold and scope, and separate authorization for paid requests. No held-out or additional live calls are included in this package. Original corpus labels, texts, split assignments, and recordings remain unchanged; automatic stamps remain disabled.

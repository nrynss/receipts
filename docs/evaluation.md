# Evaluation contract

The initial eligible claim is a first-person, explicit future commitment made by the streamer: a stated intention that contains a clear action and a commitment marker such as “I will”, “I promise”, or “I’m never going to”. Predictions, preferences, dares, bets, quotations, third-party speech, and ambiguous plans are not eligible until separately evaluated.

The labelled corpus is `testdata/eval/pairs.jsonl`. IDs are stable; `split` is fixed as `dev` or `heldout`, `label` is one of `contradiction`, `no_contradiction`, `review`, or `suppress`, and `expected` is one of `stamp`, `quiet`, or `review`. All examples are original text written for this project. No recorded streamer audio, third-party material, or model output is included.

Tune a single confidence threshold on `dev` only. Do not change held-out examples or labels in response to their results. Report the threshold and raw counts alongside these rates:

- Precision = correct automatic contradiction stamps / all automatic contradiction stamps.
- Recall = correctly stamped labelled contradictions / all labelled contradictions eligible for automatic stamping.
- False-stamp rate = incorrect automatic contradiction stamps / all labelled `no_contradiction`, `review`, and `suppress` later lines.
- Abstention rate = eligible pairs sent to quiet review or suppressed / all eligible pairs.

Review and suppression labels never count as positive automatic stamps. Sensitive-topic examples and unattributed or quoted speech expect suppression or review. The transcript fixture is a synthetic spoken script, not an audio recording.

## Initial label review

Before any model tuning, three development pairs (P0-004, P0-015 and P0-024) were changed to review: a later statement does not prove a future commitment was broken when an intervening completion or missing deadline could explain it. Four additional development pairs cover health, family and uncertain speaker attribution. The corpus now contains 44 pairs: 34 development and 10 held-out. The held-out partition and its labels are unchanged. No model results informed this review.

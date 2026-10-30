# Production model path

## Current configuration

Production settings pin the text intelligence path to GPT-6 Luna:

| Work | Adapter and model | Input boundary |
| --- | --- | --- |
| Promise extraction | Chat Completions Structured Outputs, `gpt-6-luna` | At most 32 broadcaster-attributed source segments, 2,048 characters each; at most 512 completion tokens. |
| Sensitivity | Decisions API, `gpt-6-luna` | One exact text, at most 2,048 characters. `sensitive` and `unsure` fail closed. |
| Contradiction classification | Decisions API, `gpt-6-luna` | One earlier claim and one later broadcaster source, each at most 2,048 characters. `unsure`, refusal, or invalid output is never a contradiction stamp. |
| Embeddings | `text-embedding-3-small` | Unchanged. |
| Transcription | `gpt-transcribe` | Unchanged. |

`RECEIPTS_EXTRACTION_MODEL` and `RECEIPTS_JUDGE_MODEL` default to `gpt-6-luna`; setting either to the former `gpt-4o-mini-2024-07-18` snapshot returns an actionable configuration error. The historical evaluation code retains its explicit old model ID and frozen recordings. Production construction independently requires Luna and instantiates `claims.DecisionsClient` and `judge.DecisionsClient`; the existing `claims.Client` handles extraction only.

## Metering and reservations

Requests use the default service tier. The current meter charges Chat Completions prompt tokens at the uncached GPT-6 Luna rate of 100 nanodollars per token and completion tokens at 500 nanodollars per token. It charges the full reported prompt token count at the uncached rate, without subtracting cached-token details, so prompt caching cannot lower the accounted spend. It requires a complete, consistent usage record before reporting measured cost. If usage is missing or malformed but the model output is otherwise valid, the runtime settles the conservative request reservation instead of a measured amount.

The Decisions meter charges 100 nanodollars per reported input token and zero for output tokens, matching the published Decisions pricing. It requires input, output, and total usage to be present and internally consistent. Adapter inputs are bounded far below the long-context price tier; no regional or cache-write modifier is requested. If the provider reports incomplete or malformed usage, the call is treated as unmeasured and the normal request reservation applies.

The live runtime reserves $0.025 for extraction, $0.002 for each sensitivity or judge classification, and $0.001 for each embedding. Extraction's reservation covers the capped multi-segment input and 512-token output at standard Luna rates. Classification reservations cover the Decisions adapters' bounded text and instructions using uncached input pricing with headroom. Measured usage settles against the reservation; ambiguous failures are charged conservatively according to the existing meter. These are runtime accounting limits, not predictions of actual provider spend.

## Safety and evaluation limits

The application still records contradiction outcomes as `review`. It does not convert Decisions confidence or probabilities into automatic stamping. Review of the 34-case development comparison found a false contradiction on P0-021 at confidence and selected-choice probability 1.0, because the model did not establish whether the action occurred before or after the promise. At least one Luna sensitivity result (`unsure` or `sensitive`) suppressed classification on 19/34 pairs. The reviewed corpus was annotated after the original collection and has no separate per-text sensitivity labels, so it does not establish prospective accuracy or sensitivity error rates. See the [full T5.4 comparison and evidence](evidence/t54-decisions-dev-analysis.md).

The strict 0.6/0.6 score point is only an exploratory candidate for a future parent-reviewed evaluation; it is not frozen, shipped, or used to bypass review. It produced two correct and one false stamp on the dev partition. All nonzero-stamp points in the tested grid retained at least one false stamp. Automatic stamps remain disabled until a new prospectively reviewed evaluation supports a separately approved policy.

OpenAI references: [Decisions guide](https://developers.openai.com/api/docs/guides/decisions), [GPT-6 Luna](https://developers.openai.com/api/docs/models/gpt-6-luna), [Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs), [pricing](https://developers.openai.com/api/docs/pricing), [model deprecations](https://developers.openai.com/api/docs/deprecations).

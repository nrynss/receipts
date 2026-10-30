# T5.4 evaluation: decision and remaining evidence

Automatic live stamps remain disabled. `intelligence.JudgmentOutcome` exposes the exact existing production verdict mapping for an evaluator: contradictions and unsure results go to private review; does-not-contradict is dismissed. This extraction does not change retrieval, source validation, budget enforcement, or commit-time checks. It is not a calibrated threshold. The pinned judge returns no numeric confidence, so a confidence threshold cannot be tuned against its current output.

The labelled file has 34 development and 10 held-out pairs, but the repository now has sanitized recordings for the complete corpus, but no independently reviewed labels. The T5.3 live evidence is one adapter acceptance case, not a corpus run. Generating replies from the labels would leak the answers and would not measure the model. There is therefore no independently reviewed corpus accuracy score or label-derived retrieval-miss rate to report yet; measured costs and latency are recorded below. The existing review-only policy cannot produce an automatic false stamp, but that safety property is not evidence of useful precision or recall. T5.4 remains in progress.

For a reproducible evaluation, first collect and retain sanitized, source-ID-bound model responses and embedding vectors independent of labels, including failures, usage and elapsed time. Freeze a development-selected policy before accessing held-out results. Replay retrieval with the production cosine top-three implementation and the same eligible candidate population and chronology; use the production judgment policy and source checks, not a parallel approximation. Record dataset, prompt, model, embedding model, retrieval and policy hashes, result counts and denominators, and inspect every false stamp. Keep actual model results separate from any synthetic adapter fixtures. The small synthetic corpus and its fixed labels do not establish real-world safety or generalization.

Before collecting a corpus, coordinate a spend cap. The existing live one-case command in [T5.3](t53-judge-handoff.md) makes exactly one judge request and reports measured usage. Inspect that cost before authorizing any bounded larger run. The session collector uses cumulative preflight reservations and failure-closed usage accounting; the earlier one-case adapter test is not such a collector.

Identifiers at this checkpoint: dataset SHA-256 `d8f11499801ed93d96a975069bec8d5ad5bb3a232822820e1bb7b22492f049fa`; judge source including prompt SHA-256 `e444df7a8d94d5ef0117107e58ddac0c8271ca619cc7a67ef87cb6b48dcb8022`; retrieval source SHA-256 `c6c2c50d6f39aadfb1783458ea3f86c6275bdcb6a51cbb8d2b0e72e39e92402c`; policy source SHA-256 `26cd2090e77c7e3bdbd4ec86af0485db026ed6ab065c6c72c8d9cadadd35a09b`. The judge model is `gpt-4o-mini-2024-07-18`; the production embedding model is `text-embedding-3-small`.

## Experimental collector and replay

`go run ./cmd/receipts-eval -recording /path/to/bundle.json` reports a complete partition offline. Its fixed `review-only-v1` decision accepts no threshold. It rejects missing or extra cases and wrong source IDs. The manifest binds the original dataset hash, split, models and source hashes. A `null` rate means its denominator is zero. Errors are counted separately and included as abstentions; case records retain only a stage code, elapsed time and accounted cost, never provider bodies or keys. If usage is ambiguous, the collector accounts the conservative reservation and stops; inspect provider usage before any further run.

**Limits:** this first replay indexes only each pair's own candidate. Top-three success here is tautological and does not estimate real retrieval recall; it also cannot reproduce capture eligibility or speaker verification because the original corpus lacks session and attribution metadata. The live collector supplies broadcaster attribution solely to exercise the adapter, so its pair verdicts are not a production accuracy measurement. It does not execute the full owner/session/store pipeline. The report therefore must not be used to enable stamping. `testdata/eval/sessions.json` adds separate original Codex-authored session-shaped routing fixtures with distractors and quoted, uncertain and sensitive examples. They are not independently reviewed labels or evidence of model accuracy. Their SHA-256 is `734826b070e27e9de96ef3b4b2f4607de52e5ccefe2d241cea44989c538b1216`.

After explicit budget coordination, collect one development case (up to five calls, reserved ceiling $0.077):

```sh
RECEIPTS_EVAL_SPLIT=dev RECEIPTS_EVAL_CASES=1 RECEIPTS_EVAL_BUDGET_NANO_USD=77000000 RECEIPTS_EVAL_OUTPUT=/tmp/receipts-eval-one.json go test -tags live ./internal/evaluation -run '^TestCollectLive$' -count=1 -v
```

Inspect the recorded measured usage and the provider account before proceeding. A full development collection repeats that first case; its maximum reservation-based budget is $2.618 for 34 cases. The held-out partition has 10 cases and a separate $0.77 ceiling. These ceilings are conservative reservations, not expected spend. Stop on any error or unmeasured usage, and do not merge incomplete runs into an accuracy report. The collector is deliberately behind the `live` tag and is not part of the offline gate. No corpus collection has been run.

### Session path and review gate

The session runner now executes the production `intelligence.Service` capture, indexing, top-three retrieval, source and chronology checks, and persisted outcomes against an isolated SQLite ledger. Adapter events retain input hashes, exact HTTP request-body SHA-256 digests, structured outputs, elapsed session time and measured usage; they do not retain headers, API keys or raw provider bodies. Replay validates the recorded inputs and consumes each response once. Independent calls may be replayed in a different order because generated receipt IDs affect index ordering; repeated identical inputs remain ordered. A changed source hash, missing event, unmatched source, unused event or unmeasured usage fails replay.

The fixture metadata and labels are Codex-authored, not independent ground truth. The default commands provide routing results only. Accuracy output requires a separately supplied JSON review assertion containing the exact `dataset_sha256`, a nonempty `reviewer`, and `decision` set to `independent-human-reviewed`; the file is an auditable assertion, not cryptographic proof of identity. No review assertion is included. `go run ./cmd/receipts-eval-sessions -recording /path/to/sessions.json` replays a complete split; add `-review /path/to/review.json` only after independent review. A session target-retrieval denominator includes only cases whose target was captured and whose later source reached retrieval. This small supplemental corpus is a routing experiment and is not representative population evidence.

After explicit approval of the original synthetic text payload, OpenAI destination and spend, this command collects the first development session through the production service with a maximum $0.50 durable session budget:

```sh
RECEIPTS_EVAL_CORPUS=sessions RECEIPTS_EVAL_SPLIT=dev RECEIPTS_EVAL_CASES=1 RECEIPTS_EVAL_BUDGET_NANO_USD=500000000 RECEIPTS_EVAL_OUTPUT=/tmp/receipts-session-one.json go test -tags live ./internal/evaluation -run '^TestCollectSessionsLive$' -count=1 -timeout 20m -v
```

Inspect measured usage and the provider account before any further collection. A complete development split repeats that first session and requires a separate $1.00 ceiling (`RECEIPTS_EVAL_CASES=2`, `RECEIPTS_EVAL_BUDGET_NANO_USD=1000000000`); the held-out split requires a further $1.00 ceiling after the policy is frozen. Total proposed ceiling including the first session is $2.50. Stop on failure or missing usage and reconcile provider charges before another attempt. Two approved single-session collections are documented below. The earlier pair-only command remains an adapter experiment; its retrieval rate is not meaningful.

The original 44 pairs can also run through the session path with `RECEIPTS_EVAL_CORPUS=pairs`. Each target receives three distinct, deterministic distractors from the same partition. The session chronology and broadcaster attestation are constructed for the experiment and do not establish the original source's real-world attribution. The model receives transcript text and source IDs, never labels, expected outcomes or target markers. The report's precision, recall, false-stamp and abstention rates apply only to each labelled target; distractor judgments have no ground-truth labels. Retrieval misses count captured targets absent from the actual top three; unreviewed runs do not publish label-derived rates.

The original-pair plan is one dev session at $0.50, inspect usage, then all 34 dev sessions at up to $17.00 (repeating the first), freeze the policy and independently review labels, then 10 held-out sessions at up to $5.00. This is a $22.50 aggregate ceiling; the four supplemental sessions add up to $2.00 if separately approved. For the original one-case command above, set `RECEIPTS_EVAL_CORPUS=pairs`. For the full development split set `RECEIPTS_EVAL_CASES=34` and `RECEIPTS_EVAL_BUDGET_NANO_USD=17000000000`; for held-out use `RECEIPTS_EVAL_SPLIT=heldout`, `RECEIPTS_EVAL_CASES=10` and `RECEIPTS_EVAL_BUDGET_NANO_USD=5000000000`. Use a fresh output path for every run. Report original pair sessions with `go run ./cmd/receipts-eval-sessions -dataset testdata/eval/pairs.jsonl -recording /path/to/bundle.json`. No paid calls are authorized or made by these examples.

For a lower initial budget, the full dev collection can use `RECEIPTS_EVAL_CASES=34` with `RECEIPTS_EVAL_BUDGET_NANO_USD=1000000000` ($1.00). Calls run serially: each next session requires $0.50 of remaining ceiling, and actual measured charges consume the aggregate ceiling. If it stops on budget, remaining cases are recorded as `not_collected`, so the report is a partial diagnostic, not a completed corpus evaluation. A fresh run repeats earlier sessions; no automatic retry or resume is implemented.

Held-out collection additionally requires `RECEIPTS_EVAL_FREEZE=/path/to/freeze.json` and `RECEIPTS_EVAL_DEV_REPORT=/path/to/dev-report.json`. The independently prepared freeze JSON must bind `dataset_sha256`, `policy` (`review-only-v1`), `dev_report_sha256`, `judge_source_sha256` and `policy_source_sha256` to the exact development report, dataset and source files. The collector verifies the report says `dev`, checks all hashes, and records the freeze hash. Held-out replay requires the same file via `-freeze /path/to/freeze.json`. Prepare and review this freeze before any held-out collection; it is not a claim that the labels have been independently reviewed. Use a separate review assertion for label-derived metrics. The program checks these assertions but cannot authenticate their author.

### First approved session and extraction contract

The first approved development session made six provider calls, all with measured usage, and cost $0.00036065. Its preserved [sanitized recording](evidence/t54-first-session.json) has SHA-256 `8cf41a41ebe4ce8e4fe64c8ac325349e8fc12893b93eec9a2c6d649ed8051ad6`; its [offline diagnostic](evidence/t54-first-session-diagnostic.json) consumed all six recorded events, with one of 34 development cases present, no captures and no judgments. It is not a corpus accuracy result. The recording contains post-validation adapter results rather than raw model responses, so it cannot establish why the four extraction attempts abstained.

The old extraction schema allowed any topic string while its local validator accepted only `promise`. The prompt and schema now specify `promise` for an eligible promise and `other` otherwise. The validator also accepts the explicitly worded form `I promise I will ...`, which the original target uses, while continuing to reject uncertain wording and requiring exact source text, attribution, and sensitivity checks. These changes have offline regression tests; the later retry reached capture and judgment, but did not validate multi-candidate retrieval.

For diagnostic replay of the older recording, use the historical replay implementation and source snapshot. From the repository root:

```sh
RECEIPTS_PROJECT=$(pwd)
mkdir -p /tmp/receipts-t54-original-source /tmp/receipts-t54-retry-source
git archive e5356ef | tar -x -C /tmp/receipts-t54-original-source
git archive dcb492a | tar -x -C /tmp/receipts-t54-retry-source
(cd /tmp/receipts-t54-retry-source && go run ./cmd/receipts-eval-sessions -dataset "$RECEIPTS_PROJECT/testdata/eval/pairs.jsonl" -recording "$RECEIPTS_PROJECT/docs/evidence/t54-first-session.json" -source-root /tmp/receipts-t54-original-source -allow-partial)
```

The current harness changes session timing and therefore must not replay old recordings as though they were collected with the new session policy.

### Retry and isolated capture windows

The approved retry made 24 measured calls costing $0.00068313. Its [unchanged sanitized recording](evidence/t54-retry-session.json) has SHA-256 `3de4f0befd289cdbb9396b93414a049e9e3c6c7d2298786ae31cd68bacc28efd`; the [original diagnostic](evidence/t54-retry-session-diagnostic.json) recorded four captures and three judgments all referring to P0-001. The prior capture windows overlapped, so each extraction could select the earlier claim. The recorded adapter results establish that the same source was selected; they do not establish the model's reason. The old report keyed outcomes by source ID and collapsed multiple receipts, so it cannot represent all three individual outcomes. This is not a successful multi-candidate run or corpus accuracy evidence.

The session harness now spaces synthetic candidate segments 32 seconds apart so the production 30-second lookback contains only the current candidate, with the later source placed after all capture windows. The model is not instructed to choose a target. Actual receipt IDs and source IDs are retained together, duplicate captured source IDs abort the session, and target retrieval uses that receipt-to-source mapping. A regression uses an extractor that always picks its earliest input and checks that all four candidates are captured distinctly. Source and session-policy hashes change; old recordings cannot be mixed with new ones. No live call has tested this fix. Do not start a full corpus collection based on the prior retry.

The retry can be independently reproduced using the `dcb492a` snapshot created above:

```sh
(cd /tmp/receipts-t54-retry-source && go run ./cmd/receipts-eval-sessions -dataset "$RECEIPTS_PROJECT/testdata/eval/pairs.jsonl" -recording "$RECEIPTS_PROJECT/docs/evidence/t54-retry-session.json" -allow-partial)
```


### Interrupted development collection and checkpoint recovery

An approved development collection hit the Go test default ten-minute timeout before its deferred output write; the output file is empty and is not a usable checkpoint. Read-only inspection of the preserved temporary ledger root `/tmp/TestCollectSessionsLive3333300349` found 30 session databases, 689 settled calls totaling $0.01652705, and one unresolved pending attempt. The pending attempt's provider charge is unknown. Preserve the entire ledger root and reconcile that attempt against provider usage before authorizing any repeat. These ledgers do not reconstruct model responses or labels.

The collector now writes a mode-0600 atomic, fsynced snapshot before every provider attempt, as soon as measured usage arrives, after each structured response, and after each case. It records a conservative reservation and attempt state; a reserved attempt or a measured attempt without its response blocks automatic resume. Per-attempt failures and missing usage also stop the run. Synthetic per-case SQLite ledgers live in a mode-0700 directory named by appending `.ledgers` to the checkpoint path and are deliberately retained for reconciliation. The checkpoint records their paths.

Every invocation has an overall deadline: `RECEIPTS_EVAL_MAX_RUNTIME_SECONDS` defaults to 540 and accepts 1 through 1140; it is additionally bounded to leave 45 seconds before the Go test deadline. Use `-timeout 20m` with the collector and choose a shorter runtime appropriate to the approval. On a deadline, matching completed cases are skipped, and fully recorded events in the interrupted case are replayed offline before any new call. Resume refuses changed dataset, source, policy, split, requested case count or budget; it refuses pending, response-missing, failed, or unmeasured attempts. It also stops if independent call ordering prevents safe replay. The cumulative measured charges and conservative next-attempt reservation remain within the original budget; increasing a budget requires a separately reviewed workflow and is not supported by automatic resume.

After explicit authorization and only for a valid, reconcilable checkpoint, resume the same invocation with the same settings and `RECEIPTS_EVAL_RESUME=1`. Do not use that flag on the empty file from the failed run: it has no recorded attempt states and cannot safely skip any paid calls. The partial report exposes pending attempts and unresolved reservation amounts, and cannot authorize accuracy or a held-out freeze. No additional paid calls were made while implementing checkpoint recovery.

A checkpoint also has an exclusive `.lock` file to prevent concurrent collectors. A graceful exit removes it. After a crash, the lock may remain; an operator must first verify that no collector is running and reconcile any unresolved attempt before removing the stale lock. The collector never removes stale locks automatically.


### Completed development and held-out recordings

The final recorded production-path runs completed with all usage measured and no pending attempts: development 34 cases / 758 calls / $0.01842507, and held-out 10 cases / 210 calls / $0.00522862. Their summed case latencies were 691.070 s and 189.183 s respectively; source rejection counts were 3 and 2. Both issued zero automatic stamps under the frozen review-only policy. Full offline replay matched both reports byte-for-byte. The combined measured corpus spend is $0.02365369; this excludes the earlier interrupted run's $0.01652705 settled spend and one unknown pending attempt reserved for at most $0.025.

The recordings, reports, freeze and exact replay commands are linked in [the evaluation contract](evaluation.md). Labels and the proposed rationales remain Codex-authored and unreviewed; [the review worksheet](t54-label-review.md) and blank [assertion template](t54-label-review-assertion.example.json) prepare independent review. No label-derived precision, recall, false-stamp rate, abstention rate or retrieval-miss count is claimed. The Judge has no numeric confidence output, so the decision is the frozen discrete review-only policy rather than a calibrated numerical threshold. T5.4 remains in progress and live auto-stamping remains disabled.

## Final owner review

The owner completed all 44 label decisions, with six corrections retained in a separate overlay. See [the final reviewed report and decision](evaluation.md#human-reviewed-scoring-and-final-policy-decision). Earlier statements about pending human review describe the historical checkpoint. T5.4 is complete as an evaluation and keep-disabled decision; automatic live stamping is not enabled. The post-collection audit does not retroactively validate the original labels or create a new prospective held-out result.

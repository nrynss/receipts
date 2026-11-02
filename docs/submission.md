# Judge guide and evidence

Receipts turns a viewer's `!receipt` into a saved promise and clip, then helps the channel review a possible contradiction. A streamer can approve a stamp, open an appeal and let chat vote. `!lore` reports the resulting channel history. Start with the [hosted recorded demo](https://receipts.nryn.dev/demo/); it requires no account and uses isolated, disposable fixture data, not a live stream. The [hosted demo acceptance](evidence/ta4-hosted-smoke.md) records its provider-free replay and isolation checks.

## Try your own Twitch channel

1. Open [Receipts](https://receipts.nryn.dev/account) and sign in with the Twitch account that owns your channel. Confirm the displayed channel and grant the requested access. Each account owns one channel; viewers use existing Twitch identities in chat.
2. Follow setup to add the displayed `/obs` URL as a 1920×1080 OBS Browser Source. Enter and confirm the short code shown by the source. The paired source has read-only overlay access. See [OBS setup](obs.md).
3. Use the private [control dock](https://receipts.nryn.dev/control) to manage listening, review detected contradictions and approve stamps. Automatic stamps are disabled. A live test requires your own stream and a viewer to send chat commands; it can incur provider costs. This host has capacity for one active channel, so a session may be unavailable while that capacity is in use.

Judges using the hosted service do not need an OpenAI API key or payment details. Provider usage is paid by the operator within the shared $20 cumulative service ceiling; channel and session ceilings are also $20. Live operation depends on Twitch permissions, an active broadcast and available configured spending capacity. The hosted recorded demo works without those requirements. For a separate deployment, see [operations](operations.md) and [hosted authentication setup](ta1-auth-handoff.md). For a local keyless build, fixture server and replay, see [run without keys](../README.md#run-without-keys).

## Evidence and limits

| Capability | Evidence and boundary |
|---|---|
| Capture and clip | A viewer's `!receipt` captured a promise, created a real ready Twitch clip and produced a chat confirmation. The promise was a disclosed synthetic stock voice played into the broadcast microphone. [Live acceptance](evidence/t62-live-acceptance.md) |
| Detection and approval | An owner-spoken later line produced private contradiction reviews after the sentence-within-source fix. The owner explicitly approved a review before the stamp. [Live acceptance](evidence/t62-live-acceptance.md) |
| Appeal and lore | One viewer's `!uphold` was counted, the appeal closed upheld, and OBS displayed “Upheld by Chat”. `!lore` reported the promise as broken. Pause, resume and durable void were also checked. [Live acceptance](evidence/t62-live-acceptance.md) |
| Overlay and recovery | The paired hosted OBS source displayed the saved receipt after a page refresh without re-pairing. The final hosted version still needs explicit confirmation of actual clip playback or labelled fallback and restart recovery. A visible verdict alone does not establish either. [Live acceptance](evidence/t62-live-acceptance.md); [local OBS recovery](evidence/t33-ta3-obs.json) |
| Isolation | Two owners signed in; populated live data from the first was not exposed in the second's control dock. Device revocation was also checked. [Hosted acceptance](evidence/ta4-hosted-smoke.md) |
| Model evaluation | Production uses GPT-6 Luna for extraction and Decisions sensitivity and contradiction classification. The recorded evaluation found false positives and does not establish prospective accuracy; outcomes remain review-only. [Model path and evaluation](model-migration.md) |

The actual clip player's audio and playback in the live run were not confirmed; no autoplay or unattended recovery claim is made. The owner confirmed a quiet voice-only room for the later line, and clip audio was muted. The evidence does not establish unrestricted speaker diarization or audible feedback-loop playback. The core live path passed across staged runs, while these remaining observations keep T6.2 in progress.

## Codex build attribution

The principal Codex build session ID is `01a187f1-2866-7273-aa07-ea214669f2b4`. The Codex app identifies this as a local Codex thread for this repository and returns its recorded turns. Product code, tests and fixtures were written with Codex, under the [repository protocol](../AGENTS.md). The [development diary](../dev-diary/README.md) records implementation tasks and their evidence; the [live acceptance record](evidence/t62-live-acceptance.md) describes the sentence-within-source detection repair, browser status cancellation fix, chat command routing and empty overlay snapshot fix. Their corresponding changes are `605176a`, `8ed972f`, `b4b11d1` and `abfccf5` in repository history.

No public share or export link for the build session has been verified here. A separate accessible recording demonstrating the live run and Codex build has not been verified in this guide. The existing user-prepared demo script is not reproduced here. Final video access, submission fields and candidate checks remain part of [T6.4](../dev-diary/PHASE-6-ship.md#t64-submission-readiness); this document is not a submission confirmation.

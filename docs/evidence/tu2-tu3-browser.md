# Overlay, demo and setup review

Parent review covered the combined TU.2 and TU.3 implementation.

- Landing renders a receipt on first load. The narrow evidence preview stacks without horizontal clipping; broadcast evidence remains readable at 1920 by 1080.
- Recorded appeal previews use a fixed stage-start duration. They do not count down from a historical fixture timestamp.
- Root fixture and OBS routes retain transparent overlay surfaces without human navigation.
- Mounted overlay checks passed recovery without arrival animation, fresh stamp animation, evidence transitions, clip fallback and click-to-play behavior, long-text layout and unmount cleanup.
- The local fixture server replay completed capture, contradiction, appeal and verdict without provider calls. Reset removed the replay result. After the review correction, simulated lore reported one broken promise and the preview verdict showed Upheld by Chat, matching the replay.
- The control fixture showed fresh Confirmed status. Failed or stale status remains Unknown. Review cards expose both promise and spoken evidence; the scoped API test verifies the evidence text.
- The account fixture rendered broadcast status, sole-speaker confirmation, credential-free OBS URL, pairing controls and separated danger actions.
- The full offline gate passed. API, store and demo packages also passed the race detector.

These are mounted browser and recorded fixture checks. They do not claim a new live Twitch, paid model or real OBS rehearsal. Automatic stamps remain disabled.

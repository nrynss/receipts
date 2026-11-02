---
title: Connect Twitch and OBS
description: Sign in as the channel owner, pair a read-only source, then control review.
---

## Connect the channel owner account

Open [Receipts account setup](https://receipts.nryn.dev/account) and sign in with the Twitch account that owns the channel. Check the displayed channel before granting the requested access.

Each account owns one channel. Viewers use their existing Twitch identities in chat. The hosted service supports one active channel at a time.

## Pair the OBS source

1. Copy the source URL from setup.
2. Add it to OBS as a 1920 × 1080 Browser Source.
3. Read the short pairing code shown by OBS.
4. Enter the code in setup and confirm it.
5. Confirm the source appears in the paired-device list.

The URL contains no credential. The paired source receives read-only overlay access. Revoke a source from setup when you no longer trust it.

## Review before stamps appear

Use the private [control dock](https://receipts.nryn.dev/control) to start or stop listening, inspect possible contradictions, and approve a stamp. Automatic stamps remain disabled.

The complete private control read model polls every three seconds. It preserves typed reasons and action state, then cancels stale reads. A delayed fixture showed a new review appear without refresh. Paid live verification remains pending.

An offline regression traced an approval failure to ended-session reviews appearing before current-session reviews. Approval correctly rejected those old reviews.

The queue now selects reviews from the current live session. Authenticated HTTP regressions verify approval returns 204, creates one stamp, and creates no duplicate on retry. Tests also reject ended-session reviews and requests without CSRF protection.

The fixes are deployed. Fresh hosted verification remains pending. Available logs cannot tie the historical failure to a review ID because the response and click record omitted IDs.

Pausing stamps and stopping listening use separate controls. A live test needs an active broadcast and Twitch permissions. A viewer must send the chat commands that exercise the flow.

The hosted service pays provider charges within operator-configured limits. Judges do not need an API key or payment details. See [spending and safety limits](../safety/).

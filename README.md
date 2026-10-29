# Receipts

**The stream remembers what you said.** Built with OpenAI Codex.

Chat types `!receipt` when a streamer promises something on stream. Receipts clips the moment and keeps the claim. When the streamer later goes back on it, a **RECEIPT** stamp lands on the overlay with the old clip, and chat votes on the appeal. `!lore` gives new viewers the channel's running promises and the ones that broke.

> **Status:** entry for the Codex × TwitchCon Hackathon, in development. Nothing below is implemented yet; see [`product.md`](product.md).

## Usage (planned)

```
receipts serve   # the bot, the listener and the overlay, for one channel
```

Add the overlay to OBS as a browser source. Chat commands: `!receipt`, `!lore`, `!uphold`, `!overturn`; the streamer and mods also have `!receipts off` and `!void`.

## Licence

Apache-2.0. See [`LICENSE`](LICENSE).

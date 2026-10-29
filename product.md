# Receipts

*The stream remembers what you said.*

Entry for the Codex × TwitchCon Hackathon (Devpost). Submit by 2 Nov, 14:00 PT (3 Nov, 03:30 IST). Rules in `~/work/hackathons/codex-twitchcon.md`.

## The product

Streamers make promises on stream all the time. *"I will never pick this character again." "If we hit 500 subs I'm shaving my head." "This is the last run tonight."* Chat remembers some of them, mods dig up clips by hand, and the running jokes that grow out of them are a big part of why people come back to a channel. Nothing on Twitch keeps track of them.

Receipts does. It:

1. **Takes the receipt.** Anyone in chat types `!receipt` after the streamer says something worth holding them to. Receipts clips the moment through the Twitch Clips API and pulls the claim out of the live transcript, in one line: *"Will never pick Vex again."*
2. **Listens.** The stream's audio is transcribed as it runs.
3. **Catches the contradiction.** When the streamer says something that goes back on a receipt, weeks later or minutes later, a **RECEIPT** stamp lands on the overlay with the old clip, the claim, who clipped it and when.
4. **Lets the streamer answer.** **Admit**, **Appeal** (chat votes to uphold or overturn), or **Context changed** (the streamer says why, and it goes on the record).
5. **Keeps the record.** Every receipt and verdict goes in the channel's ledger. `!lore` gives a new viewer the running promises, the broken ones and the score.

**The moment the demo is built around:** the streamer says *"ok fine, let's try Vex"*, the stamp lands with the clip of them swearing never to, and chat votes on the appeal while the streamer protests.

## Who it's for

- **Streamers** get a running bit that writes itself, and a reason for chat to listen closely.
- **Chat** gets a way to play along that rewards regulars: the best receipts are credited to whoever took them.
- **New viewers** get the lore without asking.

It works on any stream with someone talking: games, IRL, just chatting, tabletop. It is not a dev-stream tool.

## The rules that keep it fun

- **The streamer is in charge.** Stamps can be paused (`!receipts off`), a receipt can be voided by the streamer or a mod, and a cooldown keeps stamps from landing back to back.
- **A wrong stamp is worse than a missed one.** The check only fires when it's confident; anything less goes to a quiet mod queue, not the overlay.
- **Only the streamer's own words.** Receipts transcribes the broadcast, but only claims made in it can be receipted. It never records chat text as a claim.
- **No receipts on sensitive topics.** Health, family, money troubles and anything a mod voids stay off the overlay and out of `!lore`.

## Shape

One Go binary, `receipts`, and a Svelte 5 overlay it serves. One SQLite file. Runs on one box.

| Piece | Built on |
|---|---|
| Twitch events | EventSub over WebSocket: `channel.chat.message`. No public callback needed |
| Bot replies | Helix `POST /chat/messages` |
| Clips | Helix `POST /clips` (`clips:edit`, live only), then `GET /clips` until the clip is ready |
| Live audio | `streamlink` pulls the public stream, ffmpeg turns it into audio chunks (Keel `ffmpeg`, so ffmpeg dies with its job). Fallback: an OBS dock page that captures the streamer's mic in the browser (Chaaya audio) |
| Transcript | OpenAI speech-to-text into a rolling window. The receipt's claim comes from this window, so the clip itself is never transcribed |
| Contradiction check | Embeddings pick the 3–5 nearest receipts for each new line; one small model decides contradicts / doesn't / unsure. Plain cosine in Go; a channel has hundreds of receipts, not millions |
| Store | Keel `sqlite`: receipts, verdicts, votes, the transcript window |
| Overlay | Svelte 5 + Chaaya as an OBS browser source. Keel `stream` sends server-sent events; Chaaya `createEventStream` reads them |
| Clip playback | The Twitch clip embed, with `parent=` set to the overlay's own domain |
| Appeals | Counted from chat (`!uphold` / `!overturn`), not Twitch Polls: the Polls API is Affiliate and Partner only |
| Guard rails | Keel `gate` and `cost`: spend caps on the model calls |
| Hosting | Hetzner + Caddy at a `nryn.dev` subdomain. The overlay needs a public URL; the Twitch side doesn't |

Model names are settled when the build starts, from OpenAI's current list. All three jobs (speech-to-text, embeddings, the judge) run on OpenAI.

## Built with Codex

The rules require it and score it, so it shapes the work:

- **The product code is written in Codex**, in one principal thread whose session ID goes in the submission. Other threads are fine for side work; the main one carries most of the build.
- **The repository's `AGENTS.md` is Codex's brief.** It is written before the first line of code.
- **Record the Codex work as it happens**, for the video's "how it was built" footage.

## For judges (to build)

The README opens with a "For judges: start here" table: the video, a recorded stream session with its receipts and stamps, the evaluation numbers, and how to run it on your own channel.

**The evaluation:** a labelled set of receipt / later-line pairs, including near misses that sound alike but don't contradict (*"never this map"* vs *"this map is cursed"*). Report contradictions caught and false stamps, with the threshold the overlay uses.

## Hackathon rules that shape the build

- **New work only.** Keel and Chaaya are published dependencies. No code from Doyle, Lambo, Reprise or any earlier project.
- **The video must show it working on Twitch and being built with Codex**, under three minutes, on YouTube. Demo on our own channel with our own content: no third-party games, music or trademarks without permission.
- **Repository public, or private and shared with the judges.** Keys in environment variables, listed in the setup instructions.
- **No Extension.** Review takes too long and judges can't install an unreviewed one.

## Not this

- Not a clip tool. Clips are the evidence, not the product.
- Not a chatbot. The bot takes receipts and answers `!lore`; it doesn't chat.
- Not a moderation tool. It never judges chat.

## Open questions

- **Does streamlink hold a live stream for hours?** Probe it before anything depends on it; the mic dock is the fallback.
- **Clip timing.** A clip covers about the last 30 seconds. A slow `!receipt` can miss the promise on video; the transcript window keeps the text either way. Decide whether a receipt can point at a clip *and* a transcript offset.
- **What counts as a claim.** Promises, predictions, "never" and "always" statements, bets. Settle the list with the labelled set.
- **A streamer to demo with**, or our own channel.

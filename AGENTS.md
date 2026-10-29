# Agent protocol

Rules for any agent working in this repository, Codex first. The product is in [`product.md`](product.md).

## Codex builds this

- This is an entry for a hackathon that requires OpenAI Codex. The product code is written in Codex, mostly in one principal thread whose session ID is submitted.
- Planning documents may come from elsewhere. Product code, tests and fixtures come from Codex.

## Stack

- **Go** for the server, one binary, `receipts`. Go 1.27.
- **Keel** (`github.com/nrynss/keel`, v0.7.3) for `sqlite`, `stream`, `ffmpeg`, `gate`, `cost`, `id` and `config`. Use a Keel package before writing the same thing again.
- **Svelte 5 + Chaaya** (`@nrynss/chaaya`, 0.5.0) for the overlay, under `web/`. Accessible primitives from Bits UI.
- **SQLite** through Keel. No other database.
- The standard library first. A new dependency needs a stated reason in its commit message.

## The gate

- `./tools/check.sh` must pass on every commit: `gofmt`, `go vet`, `go test ./...`, and the overlay's lint, type check and tests.
- The gate needs no key, no network and no live stream. Tests run on `testdata/`: recorded EventSub frames, recorded Helix replies, transcript fixtures and recorded model replies.

## Live tests

- A live test calls a real service: Twitch, OpenAI, or a live stream. It is behind the `live` build tag and runs only with `go test -tags live ./...`.
- Live tests never run in CI and never gate a build.
- Each live run has a spend budget. Run one case first, check the cost, and stop if it would go over.

## Secrets

- Keys come from the environment, never a committed file. `.env` and `receipts.toml` are gitignored; `.env.example` holds names only.
- Never print, log, commit or paste a key or an OAuth token, including in test output, fixtures and recordings.

## Commits

- Plain messages.
- No `Co-Authored-By` trailer, and no Claude or Anthropic attribution.
- Never schedule in a document or commit: no dates, day windows or target days.

## New work only

- Everything here is written during the event. Keel and Chaaya are published dependencies; never copy code from Doyle, Lambo, Reprise or any earlier project.
- No third-party games, music or trademarks in fixtures, screenshots or the demo without permission.

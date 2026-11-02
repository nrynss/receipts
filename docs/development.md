# Development

## Prerequisites

Use Go 1.27.1 or later in the 1.27 series, Node.js 24 or later, npm, and ffmpeg/ffprobe. The offline audio checks use ffprobe on generated original WAV fixtures; they do not call a live stream. Streamlink is additionally required for the separately enabled live audio probe. The Go backend uses Keel v0.9.0; the overlay uses Svelte 5 and Chaaya 0.5.0. Install dependencies once with network access:

```sh
go mod download
cd web
npm ci
cd ..
```

Run the offline gate after installation:

```sh
./tools/check.sh
```

The gate refuses unformatted Go, runs overlay lint, type checks and tests, builds the Svelte assets, then runs Go vet/tests and builds `bin/receipts`. It disables Go module downloads and automatic toolchain downloads. On a restricted filesystem set `GOCACHE` to a writable cache location. Frontend dependency installation is deliberately separate from the offline gate.

The generated overlay is copied into `cmd/receipts/static/` before compiling the Go binary. Generated files are ignored; the Svelte source is the source of truth. `web/go.mod` is only a Go package-discovery boundary so root Go checks do not traverse npm dependencies. It defines no second backend.

For a build without checks:

```sh
./tools/build-web.sh
go build -o bin/receipts ./cmd/receipts
```

Run the fixture server:

```sh
RECEIPTS_MODE=fixture ./bin/receipts serve
```

Open `http://localhost:8080`. The fixture server serves the overlay scaffold and initializes the durable receipt ledger in the SQLite store at `data/receipts.db`; `RECEIPTS_DATABASE_PATH` overrides the database path. Startup requires an explicit `RECEIPTS_MODE=fixture` or `RECEIPTS_MODE=live`. Fixture startup has no credential references. Live mode requires channel identity, a Twitch access token, client ID and OpenAI key; transcription and embedding models remain explicitly configured, while Luna extraction and Decisions judging use pinned defaults. With `RECEIPTS_AUTH_ENABLED=true`, owner identity and Twitch tokens come from server-side OAuth instead; operator client ID/secret, token encryption key and HTTPS origin are required alongside the existing OpenAI/model configuration. See [hosted auth setup](ta1-auth-handoff.md) and the [current model path](model-migration.md). Missing settings are reported by name without values. Auth startup validates stored provider grants and schedules hourly validation; fixture startup makes no provider calls. The hosted API uses scoped ledger reads, owner controls and read-only device authorization. Live-session activation and the owner control dock are implemented; see the [live acceptance evidence](evidence/t62-live-acceptance.md) for verified behavior and remaining checks.

## Dependency choices

SvelteKit and adapter-static produce static assets that Go can embed; Vite and its Svelte plugin compile the overlay. Bits UI supplies the accessible primitives required by the stack. TypeScript and svelte-check validate source types; ESLint, its Svelte/TypeScript integrations and globals validate source files; Vitest runs overlay tests. These are build and verification tools, not additional runtime services. The npm lockfile pins transitive versions.

Compatible patched Vite and Vitest versions replace the original scaffold pins after the installation audit reported high/moderate development-tool advisories. Remaining low-severity reports involve the pinned SvelteKit 2 dependency tree. The deployed Go binary serves static assets and does not run a Node server, enhanced form actions or the Vite development server. Reassess those reports before adding server-side SvelteKit behavior; do not force-upgrade or downgrade the mandated Chaaya dependency to silence an audit count.

## Deterministic replay foundation

Run `./bin/receipts replay --fixture testdata/replay/foundation.json --db /tmp/receipts-fresh-replay.db` after building. The database path must not exist. This explicit command does not load credentials or construct provider adapters. It currently persists recorded transcript input through the real ledger; output states that product application services are not connected. The offline gate separately exercises the full receipt/stamp/appeal/lore loop.

## Hosted channel limits

`RECEIPTS_MAX_ACTIVE_CHANNELS` defaults to 2 (allowed 1–64). `RECEIPTS_GLOBAL_SPEND_LIMIT_USD` defaults to 3 and `RECEIPTS_CHANNEL_SPEND_LIMIT_USD` to 1.5. Limits must be positive and finite; a channel ceiling cannot exceed the global ceiling. Keel persists nanodollar accounting in the application SQLite file, with a lifetime ceiling (restart does not reset spend). These limits supplement the per-model settings. Owner-confirmed OBS pairing, credential issuance and revocation are implemented; local actual OBS persistence acceptance passed; hosted acceptance is complete; see [TA.4 evidence](evidence/ta4-hosted-smoke.md). See [runtime handoff](ta2-runtime-handoff.md).

## Overlay visual verification

The built fixture server accepts `?fixture=sequence` and individual overlay state queries. The default route remains transparent and idle. To run mounted lifecycle checks, start `npm run dev` in `web/` and open `/tests/overlay.html`. This dev-only harness blocks all iframe requests by CSP and simulates clip error/timeouts; it must report PASS. Repeat at 640×360 and with reduced motion enabled. See [verification evidence](evidence/t31-overlay.md). Browser fixtures exercise presentation, not real Twitch playback, authenticated SSE transport or the full judge demo.

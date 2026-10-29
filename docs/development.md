# Development

## Prerequisites

Use Go 1.27.1 or later in the 1.27 series, Node.js 24 or later, npm, and ffmpeg/ffprobe. The offline audio checks use ffprobe on generated original WAV fixtures; they do not call a live stream. Streamlink is additionally required for the separately enabled live audio probe. The Go backend uses Keel v0.7.3; the overlay uses Svelte 5 and Chaaya 0.5.0. Install dependencies once with network access:

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

Run the scaffold:

```sh
RECEIPTS_MODE=fixture ./bin/receipts serve
```

Open `http://localhost:8080`. The scaffold serves an empty overlay and initializes the durable receipt ledger in the SQLite store at `data/receipts.db`; `RECEIPTS_DATABASE_PATH` overrides the database path. Startup requires an explicit `RECEIPTS_MODE=fixture` or `RECEIPTS_MODE=live`. Fixture startup has no credential references. Live startup requires channel identity, the Twitch/OpenAI credentials and all three model IDs named in `.env.example`; missing settings are reported by name without values. The current server still serves the scaffold rather than running the live product pipeline. No Twitch or OpenAI connection is implemented by this scaffold. See the phase diary for actual task status.

## Dependency choices

SvelteKit and adapter-static produce static assets that Go can embed; Vite and its Svelte plugin compile the overlay. Bits UI supplies the accessible primitives required by the stack. TypeScript and svelte-check validate source types; ESLint, its Svelte/TypeScript integrations and globals validate source files; Vitest runs overlay tests. These are build and verification tools, not additional runtime services. The npm lockfile pins transitive versions.

Compatible patched Vite and Vitest versions replace the original scaffold pins after the installation audit reported high/moderate development-tool advisories. Remaining low-severity reports involve the pinned SvelteKit 2 dependency tree. The deployed Go binary serves static assets and does not run a Node server, enhanced form actions or the Vite development server. Reassess those reports before adding server-side SvelteKit behavior; do not force-upgrade or downgrade the mandated Chaaya dependency to silence an audit count.

## Deterministic replay foundation

Run `receipts replay --fixture testdata/replay/foundation.json --db /tmp/receipts-fresh-replay.db` after building. The database path must not exist. This explicit command does not load credentials or construct provider adapters. It currently persists recorded transcript input through the real ledger; output states that product application services are not connected. The full receipt/stamp/appeal/lore sequence remains P2 work.

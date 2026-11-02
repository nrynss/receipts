---
title: Run locally
description: Install repository dependencies and run the keyless fixture server.
---

## Prerequisites

Install Go 1.27.1 or later in the 1.27 series, Node.js 24 or later, npm, and ffmpeg with ffprobe. The normal gate uses local fixtures and needs no provider key.

## Install and check

From the repository root, download Go modules and install the overlay dependencies once. Then run the offline gate.

```sh
go mod download
cd web && npm ci && cd ..
GOCACHE=/tmp/receipts-go-cache ./tools/check.sh
```

## Start the fixture server

Build the overlay and server, then run the fixture mode.

```sh
./tools/build-web.sh
go build -o bin/receipts ./cmd/receipts
RECEIPTS_MODE=fixture ./bin/receipts serve
```

Open `http://localhost:8080` in a browser. Fixture mode does not load credentials or call Twitch or OpenAI.

## Build this documentation site

Install the pinned site dependencies, then build and check generated routes and assets.

```sh
cd site
npm ci
npm run build
npm run check:links
```

GitHub Pages builds this site with `/receipts` as its default base path. Set `SITE_BASE=/` and `SITE_ORIGIN=https://docs.example.org` for a root-domain build.

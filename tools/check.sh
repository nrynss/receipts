#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export GOPROXY=off GOTOOLCHAIN=local
unformatted=$(find cmd internal -name '*.go' -exec gofmt -l {} +)
if [ -n "$unformatted" ]; then
    printf 'Go files need formatting:\n%s\n' "$unformatted" >&2
    exit 1
fi
(cd web && npm run lint && npm run check && npm test)
./tools/build-web.sh
go vet ./...
go test ./...
go build -o bin/receipts ./cmd/receipts
if [ -f site/package.json ]; then
    if [ ! -x site/node_modules/.bin/astro ]; then
        printf 'Documentation site dependencies are missing. Run (cd site && npm ci) before the full gate.\n' >&2
        exit 1
    fi
    (cd site && npm run build && npm run check:links)
fi

#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
(cd web && npm run build)
mkdir -p cmd/receipts/static
find cmd/receipts/static -mindepth 1 ! -name .gitkeep -type f -delete
cp -R web/build/. cmd/receipts/static/

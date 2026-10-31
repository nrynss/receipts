#!/bin/sh
set -eu
if [ "$#" -ne 1 ]; then echo "usage: restore.sh /srv/receipts/backups/receipts-<stamp>.tar.gz" >&2; exit 2; fi
archive=$1
data_dir=${RECEIPTS_DATA_DIR:-/srv/receipts/data}
compose_file=${RECEIPTS_COMPOSE_FILE:-/opt/receipts/deploy/compose.yaml}
parent=$(dirname "$data_dir")
base=$(basename "$data_dir")
tmp="$parent/.${base}.restore.$$"
current="$parent/.${base}.before-restore.$$"
test -f "$archive"
sha256sum -c "$archive.sha256"
mkdir -m 700 "$tmp"
tar -xzf "$archive" -C "$tmp" --strip-components=1 "$base"
test -f "$tmp/receipts.db"
test "$(find "$tmp" -maxdepth 1 -type f | wc -l)" -le 8
docker compose -f "$compose_file" stop receipts
if [ -e "$data_dir" ]; then mv "$data_dir" "$current"; fi
if ! mv "$tmp" "$data_dir"; then
  if [ -e "$current" ]; then mv "$current" "$data_dir"; fi
  docker compose -f "$compose_file" start receipts
  exit 1
fi
if ! docker compose -f "$compose_file" up -d --no-deps receipts; then
  rm -rf "$data_dir"
  if [ -e "$current" ]; then mv "$current" "$data_dir"; fi
  docker compose -f "$compose_file" up -d --no-deps receipts
  exit 1
fi
echo "Restored $archive; previous data retained at $current"

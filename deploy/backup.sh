#!/bin/sh
set -eu
data_dir=${RECEIPTS_DATA_DIR:-/srv/receipts/data}
backup_dir=${RECEIPTS_BACKUP_DIR:-/srv/receipts/backups}
compose_file=${RECEIPTS_COMPOSE_FILE:-/opt/receipts/deploy/compose.yaml}
stamp=$(date -u +%Y%m%dT%H%M%SZ)
archive="$backup_dir/receipts-$stamp.tar.gz"
mkdir -p "$backup_dir"
chmod 700 "$backup_dir"
restart=1
cleanup() { if [ "$restart" = 1 ]; then docker compose -f "$compose_file" start receipts >/dev/null; fi; }
trap cleanup EXIT HUP INT TERM
docker compose -f "$compose_file" stop receipts
if ! tar -czf "$archive" -C "$(dirname "$data_dir")" "$(basename "$data_dir")"; then rm -f "$archive"; exit 1; fi
size=$(wc -c < "$archive")
if [ "$size" -gt 1073741824 ]; then rm -f "$archive"; echo "backup exceeds the 1 GiB safety limit" >&2; exit 1; fi
sha256sum "$archive" > "$archive.sha256"
chmod 600 "$archive" "$archive.sha256"
find "$backup_dir" -maxdepth 1 -type f -name 'receipts-*.tar.gz' -printf '%T@ %p\n' \
  | sort -rn | awk 'NR > 7 {print $2}' | while IFS= read -r old; do rm -f "$old" "$old.sha256"; done
docker compose -f "$compose_file" start receipts
restart=0
trap - EXIT HUP INT TERM
echo "Created $archive"

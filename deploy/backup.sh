#!/bin/sh
set -eu
umask 077

data_dir=${RECEIPTS_DATA_DIR:-/srv/receipts/data}
backup_dir=${RECEIPTS_BACKUP_DIR:-/srv/receipts/backups}
compose_file=${RECEIPTS_COMPOSE_FILE:-/opt/receipts/deploy/compose.yaml}
stamp=$(date -u +%Y%m%dT%H%M%SZ)-$$
archive="$backup_dir/receipts-$stamp.tar.gz"
partial="$backup_dir/.receipts-$stamp.tar.gz.partial"
sum_partial="$backup_dir/.receipts-$stamp.sha256.partial"
published=0

mkdir -p "$backup_dir"
if [ -L "$backup_dir" ] || [ ! -d "$backup_dir" ]; then
  echo "backup directory must be a real directory" >&2
  exit 1
fi
chmod 700 "$backup_dir"
if [ -L "$data_dir" ] || [ ! -d "$data_dir" ] || [ ! -f "$data_dir/receipts.db" ] || [ -L "$data_dir/receipts.db" ]; then
  echo "data directory or SQLite database is missing or unsafe" >&2
  exit 1
fi
for file in "$data_dir"/.[!.]* "$data_dir"/..?* "$data_dir"/*; do
  [ -e "$file" ] || [ -L "$file" ] || continue
  case "$(basename "$file")" in
    receipts.db|receipts.db-wal|receipts.db-shm|receipts.db-journal) ;;
    *) echo "unexpected file in SQLite data directory" >&2; exit 1 ;;
  esac
  if [ -L "$file" ] || [ ! -f "$file" ]; then echo "SQLite data directory contains a link or special file" >&2; exit 1; fi
done

stopped=0
cleanup() {
  result=$?
  rm -f "$partial" "$sum_partial"
  if [ "$published" = 0 ]; then rm -f "$archive" "$archive.sha256"; fi
  if [ "$stopped" = 1 ]; then docker compose -f "$compose_file" start receipts >/dev/null || true; fi
  exit "$result"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

stopped=1
if ! docker compose -f "$compose_file" stop receipts; then
  echo "could not stop Receipts for backup" >&2
  exit 1
fi
if ! tar -czf "$partial" -C "$(dirname "$data_dir")" "$(basename "$data_dir")"; then
  echo "could not create SQLite backup" >&2
  exit 1
fi
size=$(wc -c < "$partial")
if [ "$size" -gt 1073741824 ]; then
  echo "backup exceeds the 1 GiB safety limit" >&2
  exit 1
fi
mv "$partial" "$archive"
(cd "$backup_dir" && sha256sum "$(basename "$archive")" > "$(basename "$sum_partial")")
mv "$sum_partial" "$archive.sha256"
chmod 600 "$archive" "$archive.sha256"
published=1
find "$backup_dir" -maxdepth 1 -type f -name 'receipts-*.tar.gz' -printf '%T@ %p\n' \
  | sort -rn | awk 'NR > 7 {print $2}' | while IFS= read -r old; do rm -f "$old" "$old.sha256"; done
docker compose -f "$compose_file" start receipts
stopped=0
trap - EXIT HUP INT TERM
echo "Created $archive"

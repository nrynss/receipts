#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$tmp/bin" "$tmp/data" "$tmp/backups"
cat > "$tmp/bin/docker" <<'MOCK'
#!/bin/sh
exit 0
MOCK
chmod +x "$tmp/bin/docker"
printf 'fixture-ledger-v1\n' > "$tmp/data/receipts.db"
export PATH="$tmp/bin:$PATH"
export RECEIPTS_DATA_DIR="$tmp/data"
export RECEIPTS_BACKUP_DIR="$tmp/backups"
export RECEIPTS_COMPOSE_FILE="$tmp/compose.yaml"
touch "$RECEIPTS_COMPOSE_FILE"
sh "$root/deploy/backup.sh" >/dev/null
archive=$(find "$tmp/backups" -name 'receipts-*.tar.gz' -type f | head -1)
test -n "$archive"
printf 'broken-data\n' > "$tmp/data/receipts.db"
sh "$root/deploy/restore.sh" "$archive" >/dev/null
test "$(cat "$tmp/data/receipts.db")" = fixture-ledger-v1
test -f "$(find "$tmp" -maxdepth 1 -name '.data.before-restore.*' -type d | head -1)/receipts.db"
echo 'backup/restore fixture passed'

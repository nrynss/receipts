#!/bin/sh
set -eu
umask 077

if [ "$#" -ne 1 ]; then echo "usage: restore.sh /srv/receipts/backups/receipts-<stamp>.tar.gz" >&2; exit 2; fi
archive=$1
data_dir=${RECEIPTS_DATA_DIR:-/srv/receipts/data}
compose_file=${RECEIPTS_COMPOSE_FILE:-/opt/receipts/deploy/compose.yaml}
script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
parent=$(dirname "$data_dir")
base=$(basename "$data_dir")
test -f "$archive" && test ! -L "$archive"
test -f "$archive.sha256" && test ! -L "$archive.sha256"
archive_size=$(stat -c %s "$archive")
if [ "$archive_size" -gt 1073741824 ]; then echo "backup exceeds the 1 GiB safety limit" >&2; exit 1; fi
if [ "$(wc -l < "$archive.sha256")" -ne 1 ]; then echo "invalid backup checksum sidecar" >&2; exit 1; fi
expected_checksum=$(cut -d ' ' -f 1 < "$archive.sha256")
case "$expected_checksum" in *[!0-9a-fA-F]*|'') echo "invalid backup checksum sidecar" >&2; exit 1 ;; esac
if [ "${#expected_checksum}" -ne 64 ]; then echo "invalid backup checksum sidecar" >&2; exit 1; fi
actual_checksum=$(sha256sum -- "$archive")
actual_checksum=${actual_checksum%% *}
if [ "$expected_checksum" != "$actual_checksum" ]; then echo "backup checksum mismatch" >&2; exit 1; fi

if [ -L "$data_dir" ] || [ ! -d "$data_dir" ]; then
  echo "restore requires an existing real data directory" >&2
  exit 1
fi
restore_uid=${RECEIPTS_UID:-$(stat -c %u "$data_dir")}
restore_gid=${RECEIPTS_GID:-$(stat -c %g "$data_dir")}
restore_mode=$(stat -c %a "$data_dir")
health_timeout=${RECEIPTS_HEALTH_TIMEOUT_SECONDS:-180}
case "$restore_uid:$restore_gid:$restore_mode" in
  *[!0-9:]*|::*|:*|*:) echo "invalid restore owner or mode" >&2; exit 1 ;;
esac
case "$health_timeout" in
  *[!0-9]*|'') echo "invalid health timeout" >&2; exit 1 ;;
esac
if [ "$health_timeout" -lt 2 ] || [ "$health_timeout" -gt 300 ]; then echo "health timeout must be 2 to 300 seconds" >&2; exit 1; fi
if [ "$(id -u)" -ne 0 ] && { [ "$restore_uid" != "$(id -u)" ] || [ "$restore_gid" != "$(id -g)" ]; }; then
  echo "restore must run as root to restore the data directory owner" >&2
  exit 1
fi

mkdir -p "$parent"
tmp=$(mktemp -d "$parent/.${base}.restore.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
python3 "$script_dir/archive.py" extract "$archive" "$base" "$tmp"
staged="$tmp/$base"
chmod 600 "$staged"/*
if [ "$(id -u)" -eq 0 ]; then chown -R "$restore_uid:$restore_gid" "$staged"; fi
chmod "$restore_mode" "$staged"

current="$parent/.${base}.before-restore.$$"
failed="$parent/.${base}.failed-restore.$$"
if [ -e "$current" ] || [ -e "$failed" ]; then
  echo "restore safety path already exists" >&2
  exit 1
fi
state=prepared
rollback() {
  docker compose -f "$compose_file" stop receipts >/dev/null 2>&1 || true
  if [ -e "$data_dir" ]; then mv "$data_dir" "$failed" || return 1; fi
  if [ -e "$current" ]; then mv "$current" "$data_dir" || return 1; fi
  docker compose -f "$compose_file" up -d --no-deps --force-recreate receipts || return 1
  state=running
}
cleanup() {
  result=$?
  rm -rf "$tmp"
  case "$state" in
    stopped)
      if ! docker compose -f "$compose_file" start receipts >/dev/null; then
        echo "could not restart Receipts after restore preparation failed" >&2
        result=1
      fi
      ;;
    retained)
      if [ -e "$current" ] && ! mv "$current" "$data_dir"; then
        echo "could not restore the previous data directory" >&2
        result=1
      fi
      if ! docker compose -f "$compose_file" start receipts >/dev/null; then
        echo "could not restart Receipts after restore preparation failed" >&2
        result=1
      fi
      ;;
    swapped)
      rollback || echo "automatic rollback failed; inspect $current and $failed" >&2
      ;;
  esac
  exit "$result"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
state=stopped
trap 'exit 143' TERM
if ! docker compose -f "$compose_file" stop receipts; then
  echo "could not stop Receipts for restore" >&2
  exit 1
fi
state=retained
if ! mv "$data_dir" "$current"; then
  echo "could not retain current data directory" >&2
  exit 1
fi
state=swapped
if ! mv "$staged" "$data_dir"; then
  echo "could not install restored data directory" >&2
  exit 1
fi
state=swapped
if ! docker compose -f "$compose_file" up -d --no-deps --force-recreate receipts; then
  rollback || echo "automatic rollback could not restart Receipts; inspect $current and $failed" >&2
  echo "restored service did not start; prior data was restored" >&2
  exit 1
fi

healthy=0
attempt=0
max_attempts=$(((health_timeout + 1) / 2))
while [ "$attempt" -lt "$max_attempts" ]; do
  if health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' receipts 2>/dev/null) && [ "$health" = healthy ]; then
    healthy=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 2
done
if [ "$healthy" != 1 ]; then
  rollback || echo "automatic rollback could not restart Receipts; inspect $current and $failed" >&2
  echo "restored service did not become healthy; prior data was restored" >&2
  exit 1
fi
state=complete
trap - EXIT HUP INT TERM
rm -rf "$tmp"
echo "Restored $archive; service is healthy; previous data retained at $current"

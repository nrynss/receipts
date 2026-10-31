#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
real_docker=$(command -v docker)
if [ -z "$real_docker" ]; then echo "Docker is required for this local container fixture" >&2; exit 1; fi
tmp=$(mktemp -d)
cleanup() {
  "$real_docker" run --rm --user 0 --volume "$tmp:/fixture" alpine:3.22 \
    sh -ec "chown -R $RECEIPTS_UID:$RECEIPTS_GID /fixture" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p "$tmp/bin" "$tmp/data" "$tmp/backups"
cat > "$tmp/bin/docker" <<'MOCK'
#!/bin/sh
echo "$*" >> "$MOCK_DOCKER_LOG"
case "$*" in
  *inspect*) echo "${MOCK_HEALTH:-healthy}" ;;
esac
exit 0
MOCK
chmod +x "$tmp/bin/docker"
export PATH="$tmp/bin:$PATH"
export GOCACHE=${GOCACHE:-/tmp/receipts-go-cache}
export RECEIPTS_DATA_DIR="$tmp/data"
export RECEIPTS_BACKUP_DIR="$tmp/backups"
export RECEIPTS_ENV_FILE="$tmp/receipts.env"
export RECEIPTS_UID=$(id -u)
export RECEIPTS_GID=$(id -g)
export MOCK_DOCKER_LOG="$tmp/docker.log"
touch "$RECEIPTS_ENV_FILE"
chmod 600 "$RECEIPTS_ENV_FILE"
touch "$MOCK_DOCKER_LOG"

go run "$root/tools/deploy-fixture" create "$tmp/data/receipts.db"
ln -s /etc/passwd "$tmp/data/.unsafe-link"
before=$(wc -l < "$MOCK_DOCKER_LOG")
if sh "$root/deploy/backup.sh" >/dev/null 2>&1; then
  echo "backup unexpectedly accepted a symlink in the SQLite directory" >&2
  exit 1
fi
test "$(wc -l < "$MOCK_DOCKER_LOG")" = "$before"
rm "$tmp/data/.unsafe-link"
owner_before=$(stat -c %u "$tmp/data")
mode_before=$(stat -c %a "$tmp/data")
sh "$root/deploy/backup.sh" >/dev/null
archive=$(find "$tmp/backups" -name 'receipts-*.tar.gz' -type f | head -1)
test -n "$archive"
test "$(stat -c %a "$archive")" = 600
test "$(stat -c %a "$archive.sha256")" = 600
test -z "$(find "$tmp/backups" -name '*.partial' -type f -print -quit)"

# Reject a checksum-valid archive with a traversal path before stopping the service.
python3 - "$tmp/backups/unsafe.tar.gz" <<'PY'
import io
import sys
import tarfile

with tarfile.open(sys.argv[1], "w:gz") as archive:
    member = tarfile.TarInfo("data/../../escape")
    member.size = 1
    archive.addfile(member, io.BytesIO(b"x"))
PY
(cd "$tmp/backups" && sha256sum unsafe.tar.gz > unsafe.tar.gz.sha256)
before=$(wc -l < "$MOCK_DOCKER_LOG")
if sh "$root/deploy/restore.sh" "$tmp/backups/unsafe.tar.gz" >/dev/null 2>&1; then
  echo "unsafe archive unexpectedly restored" >&2
  exit 1
fi
test "$(wc -l < "$MOCK_DOCKER_LOG")" = "$before"
test ! -e "$tmp/escape"

# Damage the live copy, then prove backup restore recovers a real receipt ledger.
printf 'broken-ledger\n' > "$tmp/data/receipts.db"
sh "$root/deploy/restore.sh" "$archive" >/dev/null
go run "$root/tools/deploy-fixture" verify "$tmp/data/receipts.db"
test "$(stat -c %u "$tmp/data")" = "$owner_before"
test "$(stat -c %a "$tmp/data")" = "$mode_before"

# A restore whose app never reports healthy must roll back to the pre-restore ledger.
go run "$root/tools/deploy-fixture" append "$tmp/data/receipts.db" "Current ledger survives rollback"
MOCK_HEALTH=starting RECEIPTS_HEALTH_TIMEOUT_SECONDS=2 \
  sh "$root/deploy/restore.sh" "$archive" >/dev/null 2>&1 && {
    echo "unhealthy restored container was accepted" >&2
    exit 1
  }
go run "$root/tools/deploy-fixture" verify "$tmp/data/receipts.db" "Current ledger survives rollback"
# Check access under the image's actual non-root uid after the restored tree
# has gone through both the healthy restore and unhealthy rollback paths.
"$real_docker" run --rm --user 0 --volume "$tmp/data:/data" alpine:3.22 \
  sh -ec 'chown -R 10001:10001 /data'
"$real_docker" run --rm --user 10001:10001 --volume "$tmp/data:/data:ro" \
  alpine:3.22 sh -ec 'test -r /data/receipts.db'
# Deployment must retain isolation and resource bounds without Compose or a host port.
for argument in '--network proxy' '--read-only' '--cpus 1' '--memory 768m' 'traefik.http.routers.receipts.rule=Host(`receipts.nryn.dev`)'; do
  grep -F -- "$argument" "$MOCK_DOCKER_LOG" >/dev/null
done
if grep -E -- 'compose|--publish| -p ' "$MOCK_DOCKER_LOG" >/dev/null; then
  echo 'deployment used Compose or published a host port' >&2
  exit 1
fi
before=$(wc -l < "$MOCK_DOCKER_LOG")
if RECEIPTS_IMAGE=receipts:test RECEIPTS_ENV_FILE="$tmp/missing.env" sh "$root/deploy/docker-run.sh" >/dev/null 2>&1; then
  echo 'deployment accepted a missing private env file' >&2
  exit 1
fi
test "$(wc -l < "$MOCK_DOCKER_LOG")" = "$before"
echo 'Plain Docker deployment, SQLite recovery, permissions and rollback fixtures passed'

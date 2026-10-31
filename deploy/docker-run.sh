#!/bin/sh
set -eu

# Recreate only the Receipts tenant; never publish a host port.
data_dir=${RECEIPTS_DATA_DIR:-/srv/receipts/data}
env_file=${RECEIPTS_ENV_FILE:-/srv/receipts/receipts.env}
image=${RECEIPTS_IMAGE:-}
if [ -z "$image" ]; then
  image=$(docker inspect --format '{{.Image}}' receipts 2>/dev/null) || {
    echo 'Set RECEIPTS_IMAGE to the reviewed image tag or digest for first deployment.' >&2
    exit 1
  }
fi
if [ ! -d "$data_dir" ] || [ -L "$data_dir" ]; then echo 'Data directory must exist and not be a symlink.' >&2; exit 1; fi
if [ ! -f "$env_file" ] || [ -L "$env_file" ] || [ "$(stat -c %a "$env_file")" != 600 ]; then
  echo 'A private mode-0600 Receipts env file is required.' >&2
  exit 1
fi
docker image inspect "$image" >/dev/null
docker network inspect proxy >/dev/null
if docker container inspect receipts >/dev/null 2>&1; then
  docker stop receipts >/dev/null
  docker rm receipts >/dev/null
fi
exec docker run -d --name receipts --restart unless-stopped --init \
  --network proxy --read-only \
  --tmpfs /tmp:size=32m,noexec,nosuid,nodev \
  --mount "type=bind,src=$data_dir,dst=/data" \
  --env-file "$env_file" \
  --env RECEIPTS_MODE=live --env RECEIPTS_AUTH_ENABLED=true \
  --env RECEIPTS_HTTP_ADDRESS=:8080 --env RECEIPTS_DATABASE_PATH=/data/receipts.db \
  --env RECEIPTS_PUBLIC_ORIGIN=https://receipts.nryn.dev \
  --env RECEIPTS_TRANSCRIPT_RETENTION=30m --env RECEIPTS_MAX_ACTIVE_CHANNELS=1 \
  --env RECEIPTS_TRANSCRIPTION_MODEL=gpt-transcribe \
  --env RECEIPTS_EMBEDDING_MODEL=text-embedding-3-small \
  --cpus 1 --memory 768m --memory-swap 768m \
  --log-driver json-file --log-opt max-size=10m --log-opt max-file=3 \
  --label traefik.enable=true --label traefik.docker.network=proxy \
  --label 'traefik.http.routers.receipts.rule=Host(`receipts.nryn.dev`)' \
  --label traefik.http.routers.receipts.entrypoints=websecure \
  --label traefik.http.routers.receipts.tls.certresolver=letsencrypt \
  --label traefik.http.services.receipts.loadbalancer.server.port=8080 \
  "$image"

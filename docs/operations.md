# Hosted operations

This package builds the Go service and Svelte overlay into one image. The
container includes `ffmpeg` and `streamlink` for the optional audio capture
path. It runs as uid 10001, has a read-only root filesystem, and persists its
SQLite ledger only at `/srv/receipts/data`. The reverse proxy can reach port
8080 on the existing Docker `proxy` network; Docker publishes no host port.

## Host and route

The selected host is the shared Hetzner `foleyflow` server. Its operational
notes are in the separate Hetzner hosting repository. The deployment assumes
the existing `proxy` network and Traefik entrypoint named `websecure` with the
`letsencrypt` certificate resolver. `receipts.nryn.dev` is the proposed
origin. Before first deployment, an operator must confirm that hostname is
available in Cloudflare, add its proxied A record to the host, then verify
Traefik has issued a certificate. Do not alter Traefik, the firewall, or other
tenant configuration to install Receipts.

Read-only inspection confirmed that the Docker Compose CLI plugin is absent.
Python 3 is available. The deployment requires `docker compose`; arrange a
reviewed installation from the official Docker package source before running
the deployment commands. No existing tenant needs to restart for this prerequisite.

The container has only one public check: `GET /healthz` returns
`{"status":"ok"}`. It contains no account, claim, transcript, or credential
fields. `/api/health` remains owner authenticated. The overlay and demo routes
are public by design; owner controls and private transcripts require an owner
session. The paired OBS source can read the overlay only. Do not add a proxy
route to a separate control port.

## Configuration

Create `/srv/receipts/receipts.env` outside Git with mode 0600. It must contain
the Twitch application client ID and secret, a base64 encoded random 32 byte
`RECEIPTS_TOKEN_ENCRYPTION_KEY`, and `OPENAI_API_KEY`. Do not put channel
tokens or credentials in command arguments, deployment logs, or this repo.
Set `RECEIPTS_PUBLIC_ORIGIN=https://receipts.nryn.dev`; Twitch's registered
OAuth callback must be
`https://receipts.nryn.dev/auth/twitch/callback`. Authentication must remain
enabled. The compose file fixes live mode, the persistent database path, one
active channel, and 30 minute transcript retention. Configure provider spend
ceilings in the env file before enabling paid model calls.

## Install and update

Stage this repository at `/opt/receipts`, create the data and backup
directories owned by uid 10001, and prepare the env file. Review the compose
file and image source revision before running these commands from the staged
directory:

```sh
install -d -o 10001 -g 10001 -m 0750 /srv/receipts/data
install -d -o root -g root -m 0700 /srv/receipts/backups
if [ ! -e /srv/receipts/receipts.env ]; then
  install -o root -g root -m 0600 /dev/null /srv/receipts/receipts.env
else
  chown root:root /srv/receipts/receipts.env
  chmod 0600 /srv/receipts/receipts.env
fi
# Populate the env file from the names and origin described above using a
# protected editor. Never put credential values in shell history.
docker compose -f deploy/compose.yaml build
docker compose -f deploy/compose.yaml up -d
docker compose -f deploy/compose.yaml ps
curl -fsS https://receipts.nryn.dev/healthz
```

The root filesystem is read-only apart from `/data` and a 32 MiB temporary
filesystem. Container logs rotate at 10 MiB across three files. Compose caps
the container at one CPU and 768 MiB memory. The service limits live capture
to one channel and prunes transcript data after 30 minutes. The SQLite ledger
and embeddings can still grow as receipts accumulate; review `du -sh
/srv/receipts/data` and `df -h /` during operations. Keep at least 5 GiB free
on the host. Backups stop above 1 GiB and retain at most seven archives.
The restore verifier rejects archives with more than 4 GiB of expanded SQLite
files. The env bootstrap preserves an existing file's contents.

## Backup and restore

Run `deploy/backup.sh` on the host. It briefly stops Receipts for a consistent
SQLite and WAL copy, starts it again even if archive creation fails, writes a
SHA-256 sidecar, enforces a 1 GiB compressed archive limit, and keeps the
seven newest archives. It sets a private umask before creating temporary files
and atomically renames complete archives into place. Backups contain encrypted
Twitch tokens and private ledger data. Keep the backup directory mode 0700 and
include it in the operator's protected off-host backup policy if one exists.

To restore, first retain the current data directory, then run
`deploy/restore.sh /srv/receipts/backups/receipts-<stamp>.tar.gz`. It checks
the sidecar, rejects links and unexpected archive paths, stages the database,
runs SQLite `integrity_check`, and preserves the existing data directory's
numeric owner and mode before stopping the service. It keeps the pre-restore
directory, waits up to three minutes for the restored container's Docker
health check, and rolls back to the prior directory if the container fails to
start or report healthy. Run it as root when the target data directory belongs
to uid 10001. It recreates the container after swapping the directory so the
bind mount attaches to the restored directory inode. Python 3 with its standard
`tarfile` and `sqlite3` modules, GNU tar, and `sha256sum` are required on the
host. Confirm `/healthz`, sign in, and check the ledger and appeal countdowns
before resuming capture. Restore rolls back account disconnects and source
revocations that happened after the backup. After any restore, revoke affected
OBS devices and disconnect/reconnect Twitch in setup to force fresh
authorization.

Run `tools/test-deploy.sh` from the repository for a local fixture rehearsal.
It creates a synthetic receipt in a real SQLite database, verifies archive
permissions and SHA-256, rejects a traversal archive, corrupts and restores the
database, confirms SQLite integrity and receipt readability, checks the
restored data from a non-root container, and forces an unhealthy restore to
exercise rollback. It uses no Twitch or OpenAI credentials. These checks do not
count as a deployed restore or live token-revocation acceptance.

## Recovery and token revocation

Startup resets stale live authority and closes due appeals from the durable
ledger. Restarting the container does not delete receipts or reset open appeal
deadlines. To revoke a source, use setup's device list and revoke action. To
remove the linked Twitch authorization, use the account disconnect action.
Those actions stop active runtime access; a backup from before the action can
restore prior encrypted credentials and device records, so repeat revocation
after restoring an older backup.

For rollback to a prior application version, stop Receipts, retain the current
image tag, deploy the previously reviewed image, and keep the same data
directory. Take a backup before changing schema versions. If the old binary
cannot read a newer schema, restore the matching pre-upgrade archive and retain
the newer data directory for investigation.

## Clip and OBS origin acceptance

The Twitch player embeds the saved clip on the configured HTTPS origin. OBS
must use the paired `/obs` browser source on that same origin. Confirm clip
playback from the deployed hostname; if the embed is unavailable, the overlay
must show its saved transcript fallback. A page load alone does not prove clip
audio playback. Do not route OBS monitor audio back into the listening input.
Hosted-origin playback, transcript fallback and revocation remain operator
acceptance checks and are not claimed by the fixture gate.

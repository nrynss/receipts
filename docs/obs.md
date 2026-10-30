# OBS browser source

Sign in to Receipts, open setup and copy the displayed OBS source URL. Add a
Browser Source in OBS with that URL. The source displays a short-lived code;
enter it in setup to pair that source to your channel. The URL itself does not
contain a credential. If the code expires, refresh the source for a new one.

A paired source can read the public overlay only. Revoke a source from setup if
it is no longer trusted; its current stream closes and later requests fail.
Pair it again with a fresh code when needed. Pausing stamps and stopping
listening are separate controls. Signing out ends the current dashboard session.

Local real OBS acceptance passed: the owner confirmed source refresh, OBS restart and server restart without re-pairing. A persisted appeal restored the same countdown and vote counts without replaying a stamp animation. Revocation cleared the source without a refresh. See [acceptance evidence](evidence/t33-ta3-obs.json). Deployed-origin acceptance remains TA.4.

## Local OBS pairing rehearsal

The opt-in acceptance server uses a synthetic owner and the same pairing,
cookie, device, account and overlay handlers as the application. Its provider
implementation refuses every provider call. It is compiled only with the
`acceptance` build tag and binds to loopback; it is not part of the normal
`receipts` binary.

Build the static web assets, then use a locally trusted certificate for
`localhost` / `127.0.0.1`. To create one with `mkcert`, install its CA in the
current user's browser trust store and issue a loopback certificate:

```sh
./tools/build-web.sh
TRUST_STORES=nss mkcert -install
mkcert -cert-file /tmp/receipts-ta3-acceptance.pem \
  -key-file /tmp/receipts-ta3-acceptance-key.pem localhost 127.0.0.1 ::1
go run -tags acceptance ./cmd/receipts-acceptance \
  -cert /tmp/receipts-ta3-acceptance.pem \
  -key /tmp/receipts-ta3-acceptance-key.pem
```

Restart the browser/OBS after installing the CA so their Chromium trust store
reloads. `mkcert -CAROOT` shows the CA directory; do not share its
`rootCA-key.pem`. Remove the CA from the NSS trust store with
`TRUST_STORES=nss mkcert -uninstall`, then remove the two generated leaf
certificate files from `/tmp` when the rehearsal is complete.

Open the printed owner setup URL, then add the printed `/obs` URL as an OBS
Browser Source. Enter the short code shown in OBS in setup and confirm it.
Check that setup lists the paired source and that OBS shows the synthetic
stamp. In the private control dock at `/control`, click **Appeal**. Restart
this server while the appeal is open, keeping the same SQLite file. The owner
snapshot should retain the stored appeal deadline; when it is overdue, startup
recovery should close it without replaying the stamp. Refresh the Browser
Source, restart the OBS application, and verify it reconnects without another
pairing code. Revoke the source in setup; access should be denied and the
device should disappear from the list. Do not share cookies or pairing codes.
The harness uses loopback HTTPS because pairing cookies are Secure.

This local rehearsal verifies the real browser/source cookie flow without
Twitch credentials or provider traffic. It does not establish hosted HTTPS,
real-account onboarding or deployed OBS persistence; record those separately.

Account deletion removes active channel data and revokes local sessions. Backups may retain historical copies until the operator's configured
backup retention expires; deletion does not rewrite existing backups. Operators
must document their retention and restoration procedures before hosting.

# OBS browser source

Sign in to Receipts, open setup and copy the displayed OBS source URL. Add a
Browser Source in OBS with that URL. The source displays a short-lived code;
enter it in setup to pair that source to your channel. The URL itself does not
contain a credential. If the code expires, refresh the source for a new one.

A paired source can read the public overlay only. Revoke a source from setup if
it is no longer trusted; its current stream closes and later requests fail.
Pair it again with a fresh code when needed. Pausing stamps and stopping
listening are separate controls. Signing out ends the current dashboard session.

Pairing persistence through a real OBS refresh and restart remains a release
check. Browser and server fixture tests do not establish that OBS stores its
cookies across those operations.

## Local OBS pairing rehearsal

The opt-in acceptance server uses a synthetic owner and the same pairing,
cookie, device, account and overlay handlers as the application. Its provider
implementation refuses every provider call. It is compiled only with the
`acceptance` build tag and binds to loopback; it is not part of the normal
`receipts` binary.

Build the static web assets, then use a locally trusted certificate for
`localhost` / `127.0.0.1` (for example, a certificate created with `mkcert`):

```sh
./tools/build-web.sh
go run -tags acceptance ./cmd/receipts-acceptance \
  -cert /path/to/localhost.pem -key /path/to/localhost-key.pem
```

Open the printed owner setup URL, then add the printed `/obs` URL as an OBS
Browser Source. Enter the short code shown in OBS in setup and confirm it.
Check that setup lists the paired source. Refresh the Browser Source, restart
the OBS application, and restart this server while keeping the same SQLite
file. The source should reconnect without showing a new pairing code and setup
should still list the device. Revoke it in setup; the source should lose access
and the device should disappear from the list. Do not share cookies or pairing
codes. The harness uses loopback HTTPS because pairing cookies are Secure.

This local rehearsal verifies the real browser/source cookie flow without
Twitch credentials or provider traffic. It does not establish hosted HTTPS,
real-account onboarding or deployed OBS persistence; record those separately.

Account deletion removes active channel data and revokes local sessions. Backups may retain historical copies until the operator's configured
backup retention expires; deletion does not rewrite existing backups. Operators
must document their retention and restoration procedures before hosting.

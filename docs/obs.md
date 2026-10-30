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

Account deletion removes active channel data and revokes local sessions. Backups may retain historical copies until the operator's configured
backup retention expires; deletion does not rewrite existing backups. Operators
must document their retention and restoration procedures before hosting.

# TA.2 runtime and authorization handoff

Hosted API requests resolve an opaque owner session or a read-only device
credential. The API adds the active runtime's persisted channel and live-session
authority before invoking scoped readers. The reader checks account/channel
ownership in SQLite again. SSE subscriptions use the runtime's channel-local
publisher and end when the request, browser session, device credential, or
runtime is revoked.

Runtime activation uses Manager.StartVerified with a Store.VerifyLiveSession
verifier. The caller must supply a session identifier and generation from a
trusted live-status adapter; a Twitch login or request body cannot start work.
Manager.Spend validates the current generation, registers the work with the
supervisor, and meters execution through Keel's durable channel and global
budgets. Stop and account revocation cancel work and hold the capacity slot
until registered workers return.

The hosted startup composes the manager, SQLite-backed Keel budgets, scoped
ledger reader, and scoped controls. Channel count and spend ceilings are finite
settings. The overlay read, owner pause, and device read paths are available
only for a channel with an active runtime.

T2.1 receipt capture remains an application service seam, and P2/P4 have not
yet connected a live EventSub/transcript pipeline or a verified session
activation caller. Receipt void, review resolution, and receipt decisions
therefore return 501 until their application services can commit state and
events together. Hosted readiness is not claimed until those services and
the live adapter are composed and accepted.

CREATE TABLE IF NOT EXISTS accounts (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    provider_subject TEXT NOT NULL,
    login TEXT NOT NULL,
    channel_id TEXT NOT NULL UNIQUE REFERENCES channels(id),
    token_ciphertext BLOB NOT NULL,
    token_expires_at INTEGER NOT NULL,
    scopes TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('connected','reconnect_required')),
    updated_at INTEGER NOT NULL,
    UNIQUE(provider, provider_subject)
);

CREATE TABLE IF NOT EXISTS browser_sessions (
    token_hash BLOB PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    csrf_hash BLOB NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked_at INTEGER
);
CREATE INDEX IF NOT EXISTS browser_sessions_account ON browser_sessions(account_id);

CREATE TABLE IF NOT EXISTS oauth_states (
    state_hash BLOB PRIMARY KEY,
    browser_hash BLOB NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

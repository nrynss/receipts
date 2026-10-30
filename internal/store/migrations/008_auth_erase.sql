-- Short-lived hashes prevent an OAuth flow started before deletion from
-- recreating the deleted account after it completes.
CREATE TABLE auth_deletion_barriers (
    subject_hash BLOB PRIMARY KEY,
    deleted_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

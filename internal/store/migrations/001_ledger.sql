CREATE TABLE channels (
    id TEXT PRIMARY KEY,
    platform_id TEXT NOT NULL UNIQUE,
    login TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE live_sessions (
    id TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL REFERENCES channels(id),
    started_at INTEGER NOT NULL,
    ended_at INTEGER,
    state TEXT NOT NULL CHECK (state IN ('live','ended')),
    UNIQUE(id, channel_id)
);
CREATE INDEX live_sessions_channel ON live_sessions(channel_id, started_at DESC);
CREATE UNIQUE INDEX one_live_session_per_channel ON live_sessions(channel_id) WHERE state='live';

CREATE TABLE transcript_segments (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES live_sessions(id) ON DELETE CASCADE,
    start_ms INTEGER NOT NULL CHECK (start_ms >= 0),
    end_ms INTEGER NOT NULL CHECK (end_ms >= start_ms),
    text TEXT NOT NULL,
    speaker_id TEXT NOT NULL DEFAULT '',
    attribution TEXT NOT NULL DEFAULT 'uncertain',
    created_at INTEGER NOT NULL
);
CREATE INDEX transcript_session_time ON transcript_segments(session_id, start_ms);

CREATE TABLE receipts (
    id TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL REFERENCES channels(id),
    session_id TEXT NOT NULL REFERENCES live_sessions(id),
    created_at INTEGER NOT NULL,
    expires_at INTEGER,
    claim TEXT NOT NULL,
    clipped_by TEXT NOT NULL,
    validity TEXT NOT NULL CHECK (validity IN ('valid','void','expired')),
    sensitive INTEGER NOT NULL DEFAULT 0 CHECK (sensitive IN (0,1)),
    clip_status TEXT NOT NULL DEFAULT 'pending' CHECK (clip_status IN ('pending','ready','unavailable')),
    clip_id TEXT NOT NULL DEFAULT '',
    clip_url TEXT NOT NULL DEFAULT '',
    verdict TEXT NOT NULL DEFAULT 'undecided' CHECK (verdict IN ('undecided','admitted','upheld','overturned','context_changed','inconclusive')),
    version INTEGER NOT NULL DEFAULT 1,
    UNIQUE(id, channel_id),
    FOREIGN KEY(session_id, channel_id) REFERENCES live_sessions(id, channel_id)
);
CREATE INDEX receipts_public ON receipts(channel_id, created_at DESC) WHERE validity='valid' AND sensitive=0;

CREATE TABLE evidence (
    id TEXT PRIMARY KEY,
    receipt_id TEXT NOT NULL REFERENCES receipts(id) ON DELETE CASCADE,
    source_id TEXT NOT NULL,
    source_text TEXT NOT NULL,
    start_ms INTEGER NOT NULL CHECK (start_ms >= 0),
    end_ms INTEGER NOT NULL CHECK (end_ms >= start_ms),
    attribution TEXT NOT NULL,
    UNIQUE(receipt_id, source_id, start_ms, end_ms)
);

CREATE TABLE detections (
    id TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL REFERENCES channels(id),
    receipt_id TEXT NOT NULL REFERENCES receipts(id),
    session_id TEXT NOT NULL REFERENCES live_sessions(id),
    source_id TEXT NOT NULL,
    start_ms INTEGER NOT NULL,
    end_ms INTEGER NOT NULL,
    evidence_text TEXT NOT NULL,
    attribution TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('review','stamp','dismissed')),
    created_at INTEGER NOT NULL,
    UNIQUE(receipt_id, source_id, start_ms, end_ms)
);

CREATE TABLE reviews (
    id TEXT PRIMARY KEY,
    detection_id TEXT NOT NULL UNIQUE REFERENCES detections(id) ON DELETE CASCADE,
    state TEXT NOT NULL CHECK (state IN ('open','approved','rejected')),
    created_at INTEGER NOT NULL,
    decided_at INTEGER
);

CREATE TABLE appeals (
    id TEXT PRIMARY KEY,
    receipt_id TEXT NOT NULL REFERENCES receipts(id) ON DELETE CASCADE,
    state TEXT NOT NULL CHECK (state IN ('open','closed')),
    opened_at INTEGER NOT NULL,
    closes_at INTEGER NOT NULL,
    decision TEXT NOT NULL DEFAULT '',
    decided_at INTEGER,
    UNIQUE(id, receipt_id)
);
CREATE UNIQUE INDEX one_open_appeal_per_receipt ON appeals(receipt_id) WHERE state='open';

CREATE TABLE votes (
    id TEXT PRIMARY KEY,
    appeal_id TEXT NOT NULL REFERENCES appeals(id) ON DELETE CASCADE,
    voter_id TEXT NOT NULL,
    choice TEXT NOT NULL CHECK (choice IN ('uphold','overturn')),
    created_at INTEGER NOT NULL,
    UNIQUE(appeal_id, voter_id)
);

CREATE TABLE verdicts (
    id TEXT PRIMARY KEY,
    receipt_id TEXT NOT NULL REFERENCES receipts(id) ON DELETE CASCADE,
    appeal_id TEXT REFERENCES appeals(id),
    decision_key TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('admitted','upheld','overturned','context_changed','inconclusive')),
    note TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    UNIQUE(receipt_id, decision_key)
);

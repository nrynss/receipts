ALTER TABLE live_sessions ADD COLUMN broadcast_id TEXT NOT NULL DEFAULT '';
ALTER TABLE live_sessions ADD COLUMN status_checked_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE live_sessions ADD COLUMN microphone_allowed INTEGER NOT NULL DEFAULT 0 CHECK(microphone_allowed IN (0,1));
ALTER TABLE live_sessions ADD COLUMN runtime_generation INTEGER NOT NULL DEFAULT 0;

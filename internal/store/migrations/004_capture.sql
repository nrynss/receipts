CREATE TABLE receipt_commands (
  channel_id TEXT NOT NULL REFERENCES channels(id),
  command_id TEXT NOT NULL,
  session_id TEXT NOT NULL REFERENCES live_sessions(id),
  user_id TEXT NOT NULL,
  received_at INTEGER NOT NULL,
  admitted_at INTEGER NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('processing','captured','no_claim','suppressed','unavailable','throttled')),
  PRIMARY KEY(channel_id, command_id)
);
CREATE INDEX receipt_commands_user_time ON receipt_commands(channel_id,user_id,admitted_at DESC);
CREATE TABLE receipt_captures (
  receipt_id TEXT PRIMARY KEY REFERENCES receipts(id) ON DELETE CASCADE,
  channel_id TEXT NOT NULL REFERENCES channels(id),
  command_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  login TEXT NOT NULL,
  UNIQUE(channel_id, command_id),
  FOREIGN KEY(channel_id, command_id) REFERENCES receipt_commands(channel_id, command_id)
);
CREATE TABLE clip_jobs (
  id TEXT PRIMARY KEY,
  channel_id TEXT NOT NULL REFERENCES channels(id),
  receipt_id TEXT NOT NULL UNIQUE REFERENCES receipts(id) ON DELETE CASCADE,
  requested_at INTEGER NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('pending','ready','unavailable','failed')),
  clip_id TEXT NOT NULL DEFAULT '',
  clip_url TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT ''
);

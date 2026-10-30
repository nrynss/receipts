CREATE TABLE audio_chunks (
  session_id TEXT NOT NULL REFERENCES live_sessions(id) ON DELETE CASCADE,
  start_ms INTEGER NOT NULL,
  end_ms INTEGER NOT NULL,
  generation INTEGER NOT NULL,
  PRIMARY KEY(session_id,start_ms,end_ms),
  CHECK(start_ms >= 0 AND end_ms > start_ms)
);
ALTER TABLE transcript_segments ADD COLUMN timing_basis TEXT NOT NULL DEFAULT 'unspecified';
ALTER TABLE evidence ADD COLUMN timing_basis TEXT NOT NULL DEFAULT 'unspecified';

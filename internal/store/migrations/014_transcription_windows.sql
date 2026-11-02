-- Adjacent disjoint capture claims back each overlapping transcription window.
CREATE TABLE transcription_windows (
  session_id TEXT NOT NULL,
  start_ms INTEGER NOT NULL,
  end_ms INTEGER NOT NULL,
  split_ms INTEGER NOT NULL,
  generation INTEGER NOT NULL,
  PRIMARY KEY(session_id,start_ms,end_ms),
  FOREIGN KEY(session_id,start_ms,split_ms) REFERENCES audio_chunks(session_id,start_ms,end_ms),
  FOREIGN KEY(session_id,split_ms,end_ms) REFERENCES audio_chunks(session_id,start_ms,end_ms),
  CHECK(start_ms >= 0 AND split_ms > start_ms AND end_ms > split_ms AND end_ms-start_ms <= 12000)
);

CREATE TABLE stamp_history (
    detection_id TEXT PRIMARY KEY REFERENCES detections(id),
    channel_id TEXT NOT NULL REFERENCES channels(id),
    stamped_at INTEGER NOT NULL
);
CREATE INDEX stamp_history_channel_time ON stamp_history(channel_id, stamped_at DESC);

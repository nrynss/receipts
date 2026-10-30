-- Only one appeal can be active in a channel. Preserve the newest legacy
-- appeal and close any older ones without inventing a verdict.
UPDATE appeals SET state='closed', decision='cancelled', decided_at=closes_at
WHERE state='open' AND id NOT IN (
  SELECT a.id FROM appeals a JOIN receipts r ON r.id=a.receipt_id
  WHERE a.state='open' AND a.id=(SELECT b.id FROM appeals b JOIN receipts rb ON rb.id=b.receipt_id
    WHERE b.state='open' AND rb.channel_id=r.channel_id ORDER BY b.opened_at DESC,b.id DESC LIMIT 1)
);
CREATE TRIGGER one_open_appeal_per_channel BEFORE INSERT ON appeals
WHEN NEW.state='open' AND EXISTS (
  SELECT 1 FROM appeals a JOIN receipts r ON r.id=a.receipt_id
  JOIN receipts n ON n.id=NEW.receipt_id
  WHERE a.state='open' AND r.channel_id=n.channel_id
)
BEGIN SELECT RAISE(ABORT, 'channel already has an open appeal'); END;
CREATE TABLE receipt_actions (
  channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  action_key TEXT NOT NULL,
  receipt_id TEXT NOT NULL REFERENCES receipts(id) ON DELETE CASCADE,
  detection_id TEXT NOT NULL UNIQUE REFERENCES detections(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK(kind IN ('admit','context_changed','appeal')),
  note TEXT NOT NULL DEFAULT '',
  result_id TEXT NOT NULL,
  PRIMARY KEY(channel_id, action_key)
);
CREATE TABLE review_actions (
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    action_key TEXT NOT NULL,
    review_id TEXT NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
    decision TEXT NOT NULL CHECK(decision IN ('approve','reject')),
    PRIMARY KEY(channel_id, action_key)
);

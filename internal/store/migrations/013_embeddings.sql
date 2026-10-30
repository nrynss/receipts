CREATE TABLE receipt_embedding_spaces (
  channel_id TEXT PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
  model TEXT NOT NULL,
  version TEXT NOT NULL,
  dimensions INTEGER NOT NULL CHECK(dimensions > 0)
);
CREATE TABLE receipt_embeddings (
  receipt_id TEXT PRIMARY KEY REFERENCES receipts(id) ON DELETE CASCADE,
  channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  claim TEXT NOT NULL,
  vector TEXT NOT NULL,
  FOREIGN KEY(receipt_id,channel_id) REFERENCES receipts(id,channel_id)
);

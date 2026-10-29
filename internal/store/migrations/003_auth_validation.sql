ALTER TABLE accounts ADD COLUMN token_validated_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE accounts ADD COLUMN token_validation_pending INTEGER NOT NULL DEFAULT 0 CHECK (token_validation_pending IN (0,1));

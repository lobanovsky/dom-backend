UPDATE resident_tokens SET expires_at = now() WHERE expires_at IS NULL;
ALTER TABLE resident_tokens ALTER COLUMN expires_at SET NOT NULL;

DROP INDEX resident_tokens_expiry_idx;
CREATE INDEX resident_tokens_expiry_idx ON resident_tokens(expires_at);

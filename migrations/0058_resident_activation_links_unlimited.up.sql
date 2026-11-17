ALTER TABLE resident_tokens ALTER COLUMN expires_at DROP NOT NULL;
UPDATE resident_tokens SET expires_at = NULL WHERE purpose = 'activate';

DROP INDEX resident_tokens_expiry_idx;
CREATE INDEX resident_tokens_expiry_idx ON resident_tokens(expires_at) WHERE expires_at IS NOT NULL;

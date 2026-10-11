DROP TRIGGER bank_accounts_registry_codes_unique ON bank_accounts;
DROP FUNCTION bank_accounts_registry_codes_unique();
ALTER TABLE bank_accounts ADD COLUMN registry_code TEXT;
UPDATE bank_accounts SET registry_code = registry_codes[1] WHERE cardinality(registry_codes) > 0;
CREATE UNIQUE INDEX bank_accounts_registry_code_key ON bank_accounts (lower(registry_code))
    WHERE deleted_at IS NULL AND registry_code IS NOT NULL;
ALTER TABLE bank_accounts DROP COLUMN registry_codes;

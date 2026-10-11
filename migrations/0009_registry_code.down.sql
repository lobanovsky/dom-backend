ALTER TABLE payment_registries DROP CONSTRAINT payment_registries_source_check;
ALTER TABLE payment_registries ADD CONSTRAINT payment_registries_source_check CHECK (source IN ('sber'));
DROP INDEX bank_accounts_registry_code_key;
ALTER TABLE bank_accounts DROP COLUMN registry_code;

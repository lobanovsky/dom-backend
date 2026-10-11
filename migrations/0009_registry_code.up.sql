-- Код реестра в имени файла: для банков, которые не пишут номер счёта в имя файла (ВТБ: 040126_VTB_231068_001.txt).
-- Значение без даты в начале и без расширения (VTB_231068_001); сравнивается без учёта регистра.
ALTER TABLE bank_accounts ADD COLUMN registry_code TEXT;
CREATE UNIQUE INDEX bank_accounts_registry_code_key ON bank_accounts (lower(registry_code))
    WHERE deleted_at IS NULL AND registry_code IS NOT NULL;

ALTER TABLE payment_registries DROP CONSTRAINT payment_registries_source_check;
ALTER TABLE payment_registries ADD CONSTRAINT payment_registries_source_check CHECK (source IN ('sber', 'vtb'));

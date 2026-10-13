-- Выгрузки входящих платежей в реестр Сбер-формата (для загрузки во внешнюю систему).
-- Платёж, попавший в выгрузку, ссылается на неё (export_id); NULL — ещё не выгружен.
CREATE TABLE payment_exports (
    id               BIGSERIAL PRIMARY KEY,
    bank_account_id  BIGINT NOT NULL REFERENCES bank_accounts (id),
    number           INT NOT NULL CHECK (number > 0),
    file_name        TEXT NOT NULL,
    file_content     BYTEA NOT NULL,
    payments_count   INT NOT NULL CHECK (payments_count > 0),
    total_amount     NUMERIC(14, 2) NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    -- номер уникален в пределах счёта вместе с отозванными выгрузками: номера не переиспользуются
    UNIQUE (bank_account_id, number)
);
CREATE INDEX payment_exports_bank_account_id_idx ON payment_exports (bank_account_id);

ALTER TABLE incoming_payments ADD COLUMN export_id BIGINT REFERENCES payment_exports (id);
CREATE INDEX incoming_payments_export_id_idx ON incoming_payments (export_id) WHERE export_id IS NOT NULL;
-- Выбор «ещё не выгруженных» платежей с лицевым счётом из выписок.
CREATE INDEX incoming_payments_to_export_idx ON incoming_payments (bank_account_id, payment_date)
    WHERE export_id IS NULL AND registry_id IS NULL AND personal_account_id IS NOT NULL AND deleted_at IS NULL;

-- Корректировочные реестры: файл в формате реестра Сбера, где на лицевой счёт «поступила» сумма (смена собственника:
-- долг прежнего собственника закрывается условным платежом). Платежи в системе не создаются, файл только скачивается
-- и загружается во внешнюю систему; отметка resolved_at — условный платёж там уже убран (настоящая оплата пришла).
-- Номера реестров общие с payment_exports (в пределах банковского счёта), чтобы имена файлов не повторялись.
CREATE TABLE adjustment_registries (
    id                  BIGSERIAL PRIMARY KEY,
    bank_account_id     BIGINT NOT NULL REFERENCES bank_accounts (id),
    personal_account_id BIGINT NOT NULL REFERENCES personal_accounts (id),
    number              INT NOT NULL CHECK (number > 0),
    amount              NUMERIC(14, 2) NOT NULL CHECK (amount > 0),
    payer_name          TEXT NOT NULL,
    comment             TEXT,
    file_name           TEXT NOT NULL,
    file_content        BYTEA NOT NULL,
    resolved_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ,
    UNIQUE (bank_account_id, number)
);
CREATE INDEX adjustment_registries_account_idx ON adjustment_registries (personal_account_id);

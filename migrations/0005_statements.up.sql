-- Банковские выписки (xlsx): загруженный файл и связь платежей с выпиской.
-- dedup_key опознаёт одну и ту же операцию в пересекающихся выписках (например, 1–10 и 10–20 числа).

CREATE TABLE bank_statements (
    id               BIGSERIAL PRIMARY KEY,
    bank_account_id  BIGINT NOT NULL REFERENCES bank_accounts (id),
    file_name        TEXT NOT NULL,
    file_sha256      TEXT NOT NULL,
    file_content     BYTEA NOT NULL,
    period_from      DATE,
    period_to        DATE,
    opening_balance  NUMERIC(14, 2),
    closing_balance  NUMERIC(14, 2),
    debit_count      INT NOT NULL CHECK (debit_count >= 0),
    credit_count     INT NOT NULL CHECK (credit_count >= 0),
    debit_total      NUMERIC(14, 2) NOT NULL,
    credit_total     NUMERIC(14, 2) NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);
CREATE INDEX bank_statements_bank_account_id_idx ON bank_statements (bank_account_id);
CREATE UNIQUE INDEX bank_statements_file_sha256_key ON bank_statements (file_sha256);

ALTER TABLE incoming_payments
    ADD COLUMN statement_id BIGINT REFERENCES bank_statements (id),
    ADD COLUMN dedup_key    TEXT;
ALTER TABLE outgoing_payments
    ADD COLUMN statement_id BIGINT REFERENCES bank_statements (id),
    ADD COLUMN dedup_key    TEXT;

CREATE INDEX incoming_payments_statement_id_idx ON incoming_payments (statement_id);
CREATE INDEX outgoing_payments_statement_id_idx ON outgoing_payments (statement_id);
-- Без условия deleted_at: удалённая операция не «воскресает» при повторной загрузке выписки.
CREATE UNIQUE INDEX incoming_payments_dedup_key_key ON incoming_payments (bank_account_id, dedup_key) WHERE dedup_key IS NOT NULL;
CREATE UNIQUE INDEX outgoing_payments_dedup_key_key ON outgoing_payments (bank_account_id, dedup_key) WHERE dedup_key IS NOT NULL;

CREATE FUNCTION bank_statements_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('bank_accounts', NEW.bank_account_id, 'bank account');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER bank_statements_check_parents BEFORE INSERT OR UPDATE ON bank_statements
    FOR EACH ROW EXECUTE FUNCTION bank_statements_check_parents();

CREATE OR REPLACE FUNCTION incoming_payments_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('bank_accounts', NEW.bank_account_id, 'bank account');
        PERFORM assert_active('payment_registries', NEW.registry_id, 'payment registry');
        PERFORM assert_active('bank_statements', NEW.statement_id, 'bank statement');
        PERFORM assert_active('personal_accounts', NEW.personal_account_id, 'account');
        PERFORM assert_active('payment_categories', NEW.category_id, 'payment category');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION outgoing_payments_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('bank_accounts', NEW.bank_account_id, 'bank account');
        PERFORM assert_active('bank_statements', NEW.statement_id, 'bank statement');
        PERFORM assert_active('payment_categories', NEW.category_id, 'payment category');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION bank_statements_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('incoming_payments', 'statement_id', OLD.id, 'incoming payments');
    PERFORM assert_no_active('outgoing_payments', 'statement_id', OLD.id, 'outgoing payments');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER bank_statements_check_children BEFORE UPDATE ON bank_statements
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION bank_statements_check_children();

CREATE OR REPLACE FUNCTION bank_accounts_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('payment_registries', 'bank_account_id', OLD.id, 'payment registries');
    PERFORM assert_no_active('bank_statements', 'bank_account_id', OLD.id, 'bank statements');
    PERFORM assert_no_active('incoming_payments', 'bank_account_id', OLD.id, 'incoming payments');
    PERFORM assert_no_active('outgoing_payments', 'bank_account_id', OLD.id, 'outgoing payments');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

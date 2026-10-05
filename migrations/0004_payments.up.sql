-- Платежи: банковские счета организации, категории, реестры, входящие и исходящие платежи.
-- Все суммы в рублях. Мягкое удаление и проверки целостности — как в миграции 0003.

CREATE TABLE bank_accounts (
    id               BIGSERIAL PRIMARY KEY,
    organization_id  BIGINT NOT NULL REFERENCES organizations (id),
    number           TEXT NOT NULL,
    bik              TEXT,
    bank_name        TEXT,
    is_special       BOOLEAN NOT NULL DEFAULT false,
    description      TEXT,
    valid_from       DATE NOT NULL,
    valid_to         DATE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    CHECK (valid_to IS NULL OR valid_to >= valid_from)
);
CREATE INDEX bank_accounts_organization_id_idx ON bank_accounts (organization_id);
CREATE UNIQUE INDEX bank_accounts_number_key ON bank_accounts (number) WHERE deleted_at IS NULL;

CREATE TABLE payment_categories (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    direction   TEXT NOT NULL CHECK (direction IN ('incoming', 'outgoing')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX payment_categories_name_key ON payment_categories (direction, lower(name)) WHERE deleted_at IS NULL;

-- Загруженный файл реестра. file_sha256 уникален среди всех строк (в том числе удалённых):
-- тот же файл нельзя загрузить повторно.
CREATE TABLE payment_registries (
    id                BIGSERIAL PRIMARY KEY,
    bank_account_id   BIGINT NOT NULL REFERENCES bank_accounts (id),
    source            TEXT NOT NULL CHECK (source IN ('sber')),
    file_name         TEXT NOT NULL,
    file_sha256       TEXT NOT NULL,
    file_content      BYTEA NOT NULL,
    registry_number   TEXT,
    registry_date     DATE,
    payments_count    INT NOT NULL CHECK (payments_count >= 0),
    total_amount      NUMERIC(14, 2) NOT NULL,
    total_transferred NUMERIC(14, 2) NOT NULL,
    total_commission  NUMERIC(14, 2) NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at        TIMESTAMPTZ
);
CREATE INDEX payment_registries_bank_account_id_idx ON payment_registries (bank_account_id);
CREATE UNIQUE INDEX payment_registries_file_sha256_key ON payment_registries (file_sha256);

CREATE TABLE incoming_payments (
    id                   BIGSERIAL PRIMARY KEY,
    bank_account_id      BIGINT NOT NULL REFERENCES bank_accounts (id),
    registry_id          BIGINT REFERENCES payment_registries (id),
    external_id          TEXT,
    payment_date         DATE NOT NULL,
    payment_time         TIME,
    amount               NUMERIC(14, 2) NOT NULL CHECK (amount > 0),
    commission           NUMERIC(14, 2) CHECK (commission >= 0),
    payer_name           TEXT NOT NULL,
    payer_inn            TEXT,
    payer_account        TEXT,
    payer_bik            TEXT,
    payer_bank_name      TEXT,
    doc_number           TEXT,
    operation_type       TEXT,
    purpose              TEXT,
    comment              TEXT,
    personal_account_id  BIGINT REFERENCES personal_accounts (id),
    category_id          BIGINT REFERENCES payment_categories (id),
    raw_line             TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at           TIMESTAMPTZ,
    CHECK (personal_account_id IS NULL OR category_id IS NULL)
);
CREATE INDEX incoming_payments_account_date_idx ON incoming_payments (bank_account_id, payment_date);
CREATE INDEX incoming_payments_date_idx ON incoming_payments (payment_date);
CREATE INDEX incoming_payments_registry_id_idx ON incoming_payments (registry_id);
CREATE INDEX incoming_payments_personal_account_id_idx ON incoming_payments (personal_account_id);
CREATE INDEX incoming_payments_category_id_idx ON incoming_payments (category_id);
-- Без условия deleted_at: удалённый платёж не «воскресает» при повторной загрузке реестра.
CREATE UNIQUE INDEX incoming_payments_external_id_key ON incoming_payments (bank_account_id, external_id) WHERE external_id IS NOT NULL;

CREATE TABLE outgoing_payments (
    id                  BIGSERIAL PRIMARY KEY,
    bank_account_id     BIGINT NOT NULL REFERENCES bank_accounts (id),
    payment_date        DATE NOT NULL,
    amount              NUMERIC(14, 2) NOT NULL CHECK (amount > 0),
    recipient_name      TEXT NOT NULL,
    recipient_inn       TEXT,
    recipient_account   TEXT,
    recipient_bik       TEXT,
    recipient_bank_name TEXT,
    doc_number          TEXT,
    operation_type      TEXT,
    purpose             TEXT,
    comment             TEXT,
    category_id         BIGINT REFERENCES payment_categories (id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);
CREATE INDEX outgoing_payments_account_date_idx ON outgoing_payments (bank_account_id, payment_date);
CREATE INDEX outgoing_payments_date_idx ON outgoing_payments (payment_date);
CREATE INDEX outgoing_payments_category_id_idx ON outgoing_payments (category_id);

-- Нельзя сохранить (или восстановить) запись, если она ссылается на удалённую.
CREATE FUNCTION bank_accounts_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('organizations', NEW.organization_id, 'organization');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION payment_registries_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('bank_accounts', NEW.bank_account_id, 'bank account');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION incoming_payments_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('bank_accounts', NEW.bank_account_id, 'bank account');
        PERFORM assert_active('payment_registries', NEW.registry_id, 'payment registry');
        PERFORM assert_active('personal_accounts', NEW.personal_account_id, 'account');
        PERFORM assert_active('payment_categories', NEW.category_id, 'payment category');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION outgoing_payments_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('bank_accounts', NEW.bank_account_id, 'bank account');
        PERFORM assert_active('payment_categories', NEW.category_id, 'payment category');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER bank_accounts_check_parents BEFORE INSERT OR UPDATE ON bank_accounts
    FOR EACH ROW EXECUTE FUNCTION bank_accounts_check_parents();
CREATE TRIGGER payment_registries_check_parents BEFORE INSERT OR UPDATE ON payment_registries
    FOR EACH ROW EXECUTE FUNCTION payment_registries_check_parents();
CREATE TRIGGER incoming_payments_check_parents BEFORE INSERT OR UPDATE ON incoming_payments
    FOR EACH ROW EXECUTE FUNCTION incoming_payments_check_parents();
CREATE TRIGGER outgoing_payments_check_parents BEFORE INSERT OR UPDATE ON outgoing_payments
    FOR EACH ROW EXECUTE FUNCTION outgoing_payments_check_parents();

-- Запись нельзя удалить, пока на неё ссылаются неудалённые.
CREATE FUNCTION bank_accounts_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('payment_registries', 'bank_account_id', OLD.id, 'payment registries');
    PERFORM assert_no_active('incoming_payments', 'bank_account_id', OLD.id, 'incoming payments');
    PERFORM assert_no_active('outgoing_payments', 'bank_account_id', OLD.id, 'outgoing payments');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION payment_registries_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('incoming_payments', 'registry_id', OLD.id, 'incoming payments');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION payment_categories_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('incoming_payments', 'category_id', OLD.id, 'incoming payments');
    PERFORM assert_no_active('outgoing_payments', 'category_id', OLD.id, 'outgoing payments');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER bank_accounts_check_children BEFORE UPDATE ON bank_accounts
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION bank_accounts_check_children();
CREATE TRIGGER payment_registries_check_children BEFORE UPDATE ON payment_registries
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION payment_registries_check_children();
CREATE TRIGGER payment_categories_check_children BEFORE UPDATE ON payment_categories
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION payment_categories_check_children();

-- Существующие проверки: нельзя удалить организацию и лицевой счёт, пока на них ссылаются платежи и счета.
CREATE OR REPLACE FUNCTION organizations_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('buildings', 'organization_id', OLD.id, 'buildings');
    PERFORM assert_no_active('bank_accounts', 'organization_id', OLD.id, 'bank accounts');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION personal_accounts_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('account_holders', 'account_id', OLD.id, 'account holders');
    PERFORM assert_no_active('incoming_payments', 'personal_account_id', OLD.id, 'incoming payments');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

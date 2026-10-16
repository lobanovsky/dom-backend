-- Назначение услуг лицевым счетам.
-- Квитанция строится по лицевому счёту, поэтому услуга относится к типу счёта (ЖКУ, капремонт…),
-- а набор услуг счёта хранится в account_services: услуга действует с valid_from по valid_to (включительно).
-- Разовая услуга - назначение на один месяц; количество (quantity) задаётся только у услуг calc_method = 'manual'.

ALTER TABLE services
    ADD COLUMN account_purpose TEXT NOT NULL DEFAULT 'utilities'
        CHECK (account_purpose IN ('utilities', 'capital_repair', 'parking', 'other')), -- на счёт какого типа попадает услуга
    ADD COLUMN premises_kinds  TEXT[] NOT NULL DEFAULT '{}'                             -- пусто - любые помещения
        CHECK (premises_kinds <@ ARRAY['apartment', 'non_residential', 'commercial', 'parking_space', 'storage']::text[]),
    ADD COLUMN receipt_name    TEXT;                                                    -- название в квитанции (пусто - name)

CREATE TABLE account_services (
    id          BIGSERIAL PRIMARY KEY,
    account_id  BIGINT NOT NULL REFERENCES personal_accounts (id),
    service_id  BIGINT NOT NULL REFERENCES services (id),
    valid_from  DATE NOT NULL,
    valid_to    DATE,
    quantity    NUMERIC(14, 3) CHECK (quantity > 0),
    comment     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT account_services_period_check CHECK (valid_to IS NULL OR valid_to >= valid_from),
    CONSTRAINT account_services_no_overlap EXCLUDE USING gist (
        account_id WITH =, service_id WITH =, daterange(valid_from, valid_to, '[]') WITH &&) WHERE (deleted_at IS NULL)
);
CREATE INDEX account_services_service_id_idx ON account_services (service_id);

-- Согласованность назначения с услугой и счётом (см. также 0003_soft_delete для родителей).
-- Активность услуги проверяется при создании и восстановлении: закрыть датой назначение выключенной услуги можно.
CREATE FUNCTION account_services_check() RETURNS trigger AS $$
DECLARE
    svc          services%ROWTYPE;
    acc_purpose  TEXT;
    prem_kind    TEXT;
BEGIN
    IF NEW.deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;
    PERFORM assert_active('personal_accounts', NEW.account_id, 'account');
    PERFORM assert_active('services', NEW.service_id, 'service');
    SELECT * INTO svc FROM services WHERE id = NEW.service_id;
    SELECT a.purpose, p.kind INTO acc_purpose, prem_kind
        FROM personal_accounts a JOIN premises p ON p.id = a.premises_id WHERE a.id = NEW.account_id;
    IF svc.account_purpose <> acc_purpose THEN
        RAISE EXCEPTION 'service purpose does not match account purpose';
    END IF;
    IF cardinality(svc.premises_kinds) > 0 AND NOT (prem_kind = ANY (svc.premises_kinds)) THEN
        RAISE EXCEPTION 'service is not available for this premises kind';
    END IF;
    IF NOT svc.active AND (TG_OP = 'INSERT' OR OLD.deleted_at IS NOT NULL) THEN
        RAISE EXCEPTION 'service is not active';
    END IF;
    IF svc.calc_method = 'manual' AND NEW.quantity IS NULL THEN
        RAISE EXCEPTION 'quantity is required for this service';
    END IF;
    IF svc.calc_method <> 'manual' AND NEW.quantity IS NOT NULL THEN
        RAISE EXCEPTION 'quantity must be empty for this service';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER account_services_check BEFORE INSERT OR UPDATE ON account_services
    FOR EACH ROW EXECUTE FUNCTION account_services_check();

-- Счёт и услугу нельзя удалить, пока у них есть назначения.
CREATE OR REPLACE FUNCTION personal_accounts_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('account_holders', 'account_id', OLD.id, 'account holders');
    PERFORM assert_no_active('incoming_payments', 'personal_account_id', OLD.id, 'incoming payments');
    PERFORM assert_no_active('account_services', 'account_id', OLD.id, 'account services');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION services_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('service_tariffs', 'service_id', OLD.id, 'tariffs');
    PERFORM assert_no_active('account_services', 'service_id', OLD.id, 'account services');
    IF EXISTS (SELECT 1 FROM service_components c JOIN services p ON p.id = c.service_id AND p.deleted_at IS NULL WHERE c.component_id = OLD.id) THEN
        RAISE EXCEPTION 'cannot delete: has active composite services';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

-- Справочник тарифов: «Электроэнергия Т1», «ХВС» и т. п. Тариф принадлежит организации, имеет единицу измерения
-- и цены по периодам (tariff_prices). Услуга может ссылаться на тариф (services.tariff_id) вместо собственных цен
-- (service_tariffs): одна цена для нескольких услуг (электроэнергия для квартир, для машиномест, коммунальная и эксплуатационная).
-- Услуга использует либо свой тариф, либо тариф из справочника; сумма цен (sum_prices) тарифа не имеет.
CREATE TABLE tariffs (
    id               BIGSERIAL PRIMARY KEY,
    organization_id  BIGINT NOT NULL REFERENCES organizations (id),
    name             TEXT NOT NULL,
    unit             TEXT NOT NULL CHECK (unit IN ('kwh', 'm3', 'gcal', 'm2', 'piece', 'month')),
    comment          TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);
CREATE INDEX tariffs_organization_id_idx ON tariffs (organization_id);
CREATE UNIQUE INDEX tariffs_org_name_key ON tariffs (organization_id, name) WHERE deleted_at IS NULL;

CREATE TABLE tariff_prices (
    id          BIGSERIAL PRIMARY KEY,
    tariff_id   BIGINT NOT NULL REFERENCES tariffs (id),
    price       NUMERIC(14, 4) NOT NULL CHECK (price >= 0),
    valid_from  DATE NOT NULL,
    valid_to    DATE,
    comment     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT tariff_prices_period_check CHECK (valid_to IS NULL OR valid_to >= valid_from),
    CONSTRAINT tariff_prices_no_overlap EXCLUDE USING gist (
        tariff_id WITH =, daterange(valid_from, valid_to, '[]') WITH &&) WHERE (deleted_at IS NULL)
);
CREATE INDEX tariff_prices_tariff_id_idx ON tariff_prices (tariff_id, valid_from);

ALTER TABLE services ADD COLUMN tariff_id BIGINT REFERENCES tariffs (id);
CREATE INDEX services_tariff_id_idx ON services (tariff_id);
ALTER TABLE services ADD CONSTRAINT services_tariff_check CHECK (tariff_id IS NULL OR calc_method <> 'sum_prices');

-- Цена единицы услуги на дату без учёта состава: тариф из справочника, если он выбран, иначе собственный тариф услуги.
CREATE FUNCTION service_unit_price_on(sid BIGINT, d DATE) RETURNS NUMERIC AS $$
    SELECT CASE WHEN s.tariff_id IS NOT NULL THEN
        (SELECT p.price FROM tariff_prices p
          WHERE p.tariff_id = s.tariff_id AND p.deleted_at IS NULL AND p.valid_from <= d AND (p.valid_to IS NULL OR p.valid_to >= d))
    ELSE
        (SELECT t.price FROM service_tariffs t
          WHERE t.service_id = s.id AND t.deleted_at IS NULL AND t.valid_from <= d AND (t.valid_to IS NULL OR t.valid_to >= d))
    END
    FROM services s WHERE s.id = sid
$$ LANGUAGE sql STABLE;

-- Цена единицы услуги на дату: у sum_prices - сумма цен слагаемых (NULL, если у какого-то нет цены), у остальных - своя цена.
CREATE OR REPLACE FUNCTION service_price_on(sid BIGINT, d DATE) RETURNS NUMERIC AS $$
    SELECT CASE WHEN s.calc_method = 'sum_prices' THEN
        (SELECT CASE WHEN count(*) = 0 OR count(service_unit_price_on(c.component_id, d)) < count(*) THEN NULL
                     ELSE sum(service_unit_price_on(c.component_id, d)) END
           FROM service_components c WHERE c.service_id = s.id)
    ELSE service_unit_price_on(s.id, d)
    END
    FROM services s WHERE s.id = sid
$$ LANGUAGE sql STABLE;

CREATE FUNCTION tariffs_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('organizations', NEW.organization_id, 'organization');
        -- организацию и единицу тарифа нельзя менять, пока по нему считаются услуги
        IF TG_OP = 'UPDATE' AND (NEW.organization_id <> OLD.organization_id OR NEW.unit <> OLD.unit)
           AND EXISTS (SELECT 1 FROM services WHERE tariff_id = NEW.id AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'tariff is used by services';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION tariff_prices_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('tariffs', NEW.tariff_id, 'tariff');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION tariffs_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('tariff_prices', 'tariff_id', OLD.id, 'prices');
    PERFORM assert_no_active('services', 'tariff_id', OLD.id, 'services');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER tariffs_check_parents BEFORE INSERT OR UPDATE ON tariffs
    FOR EACH ROW EXECUTE FUNCTION tariffs_check_parents();
CREATE TRIGGER tariff_prices_check_parents BEFORE INSERT OR UPDATE ON tariff_prices
    FOR EACH ROW EXECUTE FUNCTION tariff_prices_check_parents();
CREATE TRIGGER tariffs_check_children BEFORE UPDATE ON tariffs
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION tariffs_check_children();

-- Услуга со ссылкой на тариф: тариф живой, той же организации и с той же единицей, собственных цен нет.
CREATE OR REPLACE FUNCTION services_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('organizations', NEW.organization_id, 'organization');
        IF NEW.tariff_id IS NOT NULL THEN
            PERFORM assert_active('tariffs', NEW.tariff_id, 'tariff');
            IF NOT EXISTS (SELECT 1 FROM tariffs WHERE id = NEW.tariff_id AND organization_id = NEW.organization_id AND unit = NEW.unit) THEN
                RAISE EXCEPTION 'tariff does not match service organization or unit';
            END IF;
            IF NEW.id IS NOT NULL AND EXISTS (SELECT 1 FROM service_tariffs WHERE service_id = NEW.id AND deleted_at IS NULL) THEN
                RAISE EXCEPTION 'service has own tariffs';
            END IF;
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

-- Собственные цены нельзя завести у услуги, которая берёт цену из справочника.
CREATE OR REPLACE FUNCTION service_tariffs_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('services', NEW.service_id, 'service');
        IF EXISTS (SELECT 1 FROM services WHERE id = NEW.service_id AND tariff_id IS NOT NULL) THEN
            RAISE EXCEPTION 'service uses a catalog tariff';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

-- Организацию нельзя удалить, пока у неё есть тарифы.
CREATE OR REPLACE FUNCTION organizations_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('buildings', 'organization_id', OLD.id, 'buildings');
    PERFORM assert_no_active('bank_accounts', 'organization_id', OLD.id, 'bank accounts');
    PERFORM assert_no_active('services', 'organization_id', OLD.id, 'services');
    PERFORM assert_no_active('tariffs', 'organization_id', OLD.id, 'tariffs');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

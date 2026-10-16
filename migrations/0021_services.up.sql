-- Услуги (коммунальные, эксплуатационные, дополнительные) и их тарифы с периодами действия.
-- Услуга принадлежит организации (ТСН). calc_method описывает, как считается объём услуги:
--   fixed             - цена за единицу × 1 в месяц (домофон, консьерж);
--   per_area          - цена × площадь помещения (содержание и ремонт);
--   individual_meter  - цена × расход по индивидуальному счётчику (meter_resource + meter_tariff);
--   common_meter      - расход общедомового счётчика делится между помещениями по площади;
--   sum               - сумма других услуг (service_components), собственных тарифов нет;
--   manual            - количество вводится при выставлении (изготовление ключей).
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE services (
    id               BIGSERIAL PRIMARY KEY,
    organization_id  BIGINT NOT NULL REFERENCES organizations (id),
    kind             TEXT NOT NULL CHECK (kind IN ('utility', 'maintenance', 'additional')),
    name             TEXT NOT NULL,
    unit             TEXT NOT NULL CHECK (unit IN ('kwh', 'm3', 'gcal', 'm2', 'piece', 'month')),
    calc_method      TEXT NOT NULL CHECK (calc_method IN ('fixed', 'per_area', 'individual_meter', 'common_meter', 'sum', 'manual')),
    meter_resource   TEXT CHECK (meter_resource IN ('cold_water', 'hot_water', 'electricity')),
    meter_tariff     INT,
    position         INT NOT NULL DEFAULT 0, -- порядок строк в квитанции
    active           BOOLEAN NOT NULL DEFAULT true,
    comment          TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    CONSTRAINT services_meter_check CHECK (
        (calc_method IN ('individual_meter', 'common_meter')
            AND meter_resource IS NOT NULL AND meter_tariff BETWEEN 1 AND 3
            AND (meter_resource = 'electricity' OR meter_tariff = 1))
        OR (calc_method NOT IN ('individual_meter', 'common_meter') AND meter_resource IS NULL AND meter_tariff IS NULL)),
    CONSTRAINT services_per_area_unit_check CHECK (calc_method <> 'per_area' OR unit = 'm2')
);
CREATE INDEX services_organization_id_idx ON services (organization_id);
CREATE UNIQUE INDEX services_org_kind_name_key ON services (organization_id, kind, name) WHERE deleted_at IS NULL;

-- Состав услуги calc_method = 'sum'. Связь без собственной отметки удаления: состав заменяется целиком.
CREATE TABLE service_components (
    service_id    BIGINT NOT NULL REFERENCES services (id),
    component_id  BIGINT NOT NULL REFERENCES services (id),
    PRIMARY KEY (service_id, component_id),
    CHECK (service_id <> component_id)
);
CREATE INDEX service_components_component_id_idx ON service_components (component_id);

-- Тариф: цена за единицу услуги в период [valid_from, valid_to] (valid_to пуст - действует до сих пор).
-- Периоды одной услуги не пересекаются.
CREATE TABLE service_tariffs (
    id          BIGSERIAL PRIMARY KEY,
    service_id  BIGINT NOT NULL REFERENCES services (id),
    price       NUMERIC(14, 4) NOT NULL CHECK (price >= 0),
    valid_from  DATE NOT NULL,
    valid_to    DATE,
    comment     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    CONSTRAINT service_tariffs_period_check CHECK (valid_to IS NULL OR valid_to >= valid_from),
    CONSTRAINT service_tariffs_no_overlap EXCLUDE USING gist (
        service_id WITH =, daterange(valid_from, valid_to, '[]') WITH &&) WHERE (deleted_at IS NULL)
);
CREATE INDEX service_tariffs_service_id_idx ON service_tariffs (service_id, valid_from);

-- Цена единицы услуги на дату; у составной услуги - сумма цен компонентов (NULL, если у какого-то нет тарифа).
CREATE FUNCTION service_price_on(sid BIGINT, d DATE) RETURNS NUMERIC AS $$
    SELECT CASE WHEN s.calc_method = 'sum' THEN
        (SELECT CASE WHEN count(*) = 0 OR count(t.price) < count(*) THEN NULL ELSE sum(t.price) END
           FROM service_components c
           LEFT JOIN LATERAL (
               SELECT price FROM service_tariffs t
               WHERE t.service_id = c.component_id AND t.deleted_at IS NULL AND t.valid_from <= d AND (t.valid_to IS NULL OR t.valid_to >= d)
           ) t ON true
           WHERE c.service_id = s.id)
    ELSE
        (SELECT price FROM service_tariffs t
          WHERE t.service_id = s.id AND t.deleted_at IS NULL AND t.valid_from <= d AND (t.valid_to IS NULL OR t.valid_to >= d))
    END
    FROM services s WHERE s.id = sid
$$ LANGUAGE sql STABLE;

-- Целостность живых и удалённых записей (см. 0003_soft_delete).
CREATE FUNCTION services_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('organizations', NEW.organization_id, 'organization');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION service_tariffs_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('services', NEW.service_id, 'service');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION services_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('service_tariffs', 'service_id', OLD.id, 'tariffs');
    IF EXISTS (SELECT 1 FROM service_components c JOIN services p ON p.id = c.service_id AND p.deleted_at IS NULL WHERE c.component_id = OLD.id) THEN
        RAISE EXCEPTION 'cannot delete: has active composite services';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER services_check_parents BEFORE INSERT OR UPDATE ON services
    FOR EACH ROW EXECUTE FUNCTION services_check_parents();
CREATE TRIGGER service_tariffs_check_parents BEFORE INSERT OR UPDATE ON service_tariffs
    FOR EACH ROW EXECUTE FUNCTION service_tariffs_check_parents();
CREATE TRIGGER services_check_children BEFORE UPDATE ON services
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION services_check_children();

-- Организацию нельзя удалить, пока у неё есть услуги.
CREATE OR REPLACE FUNCTION organizations_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('buildings', 'organization_id', OLD.id, 'buildings');
    PERFORM assert_no_active('bank_accounts', 'organization_id', OLD.id, 'bank accounts');
    PERFORM assert_no_active('services', 'organization_id', OLD.id, 'services');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

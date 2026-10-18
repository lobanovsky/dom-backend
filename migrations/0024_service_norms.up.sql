-- Услуга «площадь × норматив × тариф» (отопление): объём = площадь помещения × норматив дома, цена - тариф за Гкал.
-- Норматив (Гкал на м²) задаётся для каждого дома своим периодом действия, периоды одного дома не пересекаются.
ALTER TABLE services DROP CONSTRAINT services_calc_method_check;
ALTER TABLE services ADD CONSTRAINT services_calc_method_check
    CHECK (calc_method IN ('fixed', 'per_area', 'per_area_norm', 'individual_meter', 'common_meter', 'sum_volumes', 'sum_prices', 'manual'));
ALTER TABLE services ADD CONSTRAINT services_per_area_norm_unit_check CHECK (calc_method <> 'per_area_norm' OR unit = 'gcal');

CREATE TABLE service_norms (
    id           BIGSERIAL PRIMARY KEY,
    service_id   BIGINT NOT NULL REFERENCES services (id),
    building_id  BIGINT NOT NULL REFERENCES buildings (id),
    value        NUMERIC(10, 5) NOT NULL CHECK (value > 0),
    valid_from   DATE NOT NULL,
    valid_to     DATE,
    comment      TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ,
    CONSTRAINT service_norms_period_check CHECK (valid_to IS NULL OR valid_to >= valid_from),
    CONSTRAINT service_norms_no_overlap EXCLUDE USING gist (
        service_id WITH =, building_id WITH =, daterange(valid_from, valid_to, '[]') WITH &&) WHERE (deleted_at IS NULL)
);
CREATE INDEX service_norms_service_id_idx ON service_norms (service_id, building_id, valid_from);
CREATE INDEX service_norms_building_id_idx ON service_norms (building_id);

-- Норматив бывает только у услуги «площадь × норматив» и только для дома её организации.
CREATE FUNCTION service_norms_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('services', NEW.service_id, 'service');
        PERFORM assert_active('buildings', NEW.building_id, 'building');
        IF NOT EXISTS (SELECT 1 FROM services WHERE id = NEW.service_id AND calc_method = 'per_area_norm') THEN
            RAISE EXCEPTION 'service has no norms';
        END IF;
        IF NOT EXISTS (SELECT 1 FROM services s JOIN buildings b ON b.id = NEW.building_id
                       WHERE s.id = NEW.service_id AND s.organization_id = b.organization_id) THEN
            RAISE EXCEPTION 'building belongs to another organization';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER service_norms_check_parents BEFORE INSERT OR UPDATE ON service_norms
    FOR EACH ROW EXECUTE FUNCTION service_norms_check_parents();

-- Услугу и дом нельзя удалить, пока у них есть действующие нормативы.
CREATE OR REPLACE FUNCTION services_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('service_tariffs', 'service_id', OLD.id, 'tariffs');
    PERFORM assert_no_active('account_services', 'service_id', OLD.id, 'account services');
    PERFORM assert_no_active('service_norms', 'service_id', OLD.id, 'norms');
    IF EXISTS (SELECT 1 FROM service_components c JOIN services p ON p.id = c.service_id AND p.deleted_at IS NULL WHERE c.component_id = OLD.id) THEN
        RAISE EXCEPTION 'cannot delete: has active composite services';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION buildings_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('premises', 'building_id', OLD.id, 'premises');
    PERFORM assert_no_active('meters', 'building_id', OLD.id, 'meters');
    PERFORM assert_no_active('service_norms', 'building_id', OLD.id, 'service norms');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

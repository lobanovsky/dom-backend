CREATE OR REPLACE FUNCTION buildings_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('premises', 'building_id', OLD.id, 'premises');
    PERFORM assert_no_active('meters', 'building_id', OLD.id, 'meters');
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

DROP TABLE service_norms;
DROP FUNCTION service_norms_check_parents();

-- Услуги «площадь × норматив» не имеют аналога в прежней схеме: возвращаем их как «за м²» (единица м²).
UPDATE services SET calc_method = 'per_area', unit = 'm2' WHERE calc_method = 'per_area_norm';
ALTER TABLE services DROP CONSTRAINT services_per_area_norm_unit_check;
ALTER TABLE services DROP CONSTRAINT services_calc_method_check;
ALTER TABLE services ADD CONSTRAINT services_calc_method_check
    CHECK (calc_method IN ('fixed', 'per_area', 'individual_meter', 'common_meter', 'sum_volumes', 'sum_prices', 'manual'));

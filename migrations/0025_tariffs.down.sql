CREATE OR REPLACE FUNCTION services_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('organizations', NEW.organization_id, 'organization');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION service_tariffs_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('services', NEW.service_id, 'service');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

-- Услуги со ссылкой на тариф теряют цену: копируем цены справочника в собственные тарифы услуг.
INSERT INTO service_tariffs (service_id, price, valid_from, valid_to, comment)
SELECT s.id, p.price, p.valid_from, p.valid_to, p.comment
FROM services s JOIN tariff_prices p ON p.tariff_id = s.tariff_id AND p.deleted_at IS NULL
WHERE s.tariff_id IS NOT NULL AND s.deleted_at IS NULL;


CREATE OR REPLACE FUNCTION organizations_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('buildings', 'organization_id', OLD.id, 'buildings');
    PERFORM assert_no_active('bank_accounts', 'organization_id', OLD.id, 'bank accounts');
    PERFORM assert_no_active('services', 'organization_id', OLD.id, 'services');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION service_price_on(sid BIGINT, d DATE) RETURNS NUMERIC AS $$
    SELECT CASE WHEN s.calc_method = 'sum_prices' THEN
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

DROP FUNCTION service_unit_price_on(BIGINT, DATE);
ALTER TABLE services DROP CONSTRAINT services_tariff_check;
ALTER TABLE services DROP COLUMN tariff_id;
DROP TABLE tariff_prices;
DROP TABLE tariffs;
DROP FUNCTION tariffs_check_parents();
DROP FUNCTION tariff_prices_check_parents();
DROP FUNCTION tariffs_check_children();

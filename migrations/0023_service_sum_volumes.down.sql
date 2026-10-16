-- Обе составные услуги возвращаются к прежнему 'sum'; собственные тарифы бывших sum_volumes остаются в таблице, но не используются.
CREATE OR REPLACE FUNCTION service_price_on(sid BIGINT, d DATE) RETURNS NUMERIC AS $$
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

ALTER TABLE services DROP CONSTRAINT services_calc_method_check;
UPDATE services SET calc_method = 'sum' WHERE calc_method IN ('sum_prices', 'sum_volumes');
ALTER TABLE services ADD CONSTRAINT services_calc_method_check
    CHECK (calc_method IN ('fixed', 'per_area', 'individual_meter', 'common_meter', 'sum', 'manual'));

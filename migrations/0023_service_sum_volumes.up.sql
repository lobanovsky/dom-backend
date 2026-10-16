-- Составные услуги бывают двух видов:
--   sum_volumes - объём равен сумме объёмов других услуг, цена своя (водоотведение = ХВС + ХВС для ГВС, тариф на водоотведение);
--   sum_prices  - сумма цен других услуг, собственных тарифов нет (прежний calc_method = 'sum').
ALTER TABLE services DROP CONSTRAINT services_calc_method_check;
UPDATE services SET calc_method = 'sum_prices' WHERE calc_method = 'sum';
ALTER TABLE services ADD CONSTRAINT services_calc_method_check
    CHECK (calc_method IN ('fixed', 'per_area', 'individual_meter', 'common_meter', 'sum_volumes', 'sum_prices', 'manual'));

-- Цена единицы услуги на дату: у sum_prices - сумма цен слагаемых (NULL, если у какого-то нет тарифа), у остальных - собственный тариф.
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

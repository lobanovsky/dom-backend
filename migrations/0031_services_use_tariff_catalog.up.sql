-- Цены услуг хранятся только в справочнике тарифов (tariffs / tariff_prices). У каждой действующей услуги,
-- кроме «суммы цен», теперь есть тариф: собственные цены (service_tariffs) переносятся в новый тариф с названием услуги,
-- у услуг без цен создаётся пустой тариф. Если название уже занято в организации, к нему добавляется « (услуга № id)».
-- Таблица service_tariffs остаётся как архив (уже перенесённые строки помечены удалёнными), новые строки через API не создаются.
DO $$
DECLARE
    s     RECORD;
    tid   BIGINT;
    tname TEXT;
BEGIN
    FOR s IN SELECT id, organization_id, name, unit FROM services
              WHERE deleted_at IS NULL AND tariff_id IS NULL AND calc_method <> 'sum_prices' ORDER BY id LOOP
        tname := s.name;
        IF EXISTS (SELECT 1 FROM tariffs WHERE organization_id = s.organization_id AND name = tname AND deleted_at IS NULL) THEN
            tname := s.name || ' (услуга № ' || s.id || ')';
        END IF;
        INSERT INTO tariffs (organization_id, name, unit) VALUES (s.organization_id, tname, s.unit) RETURNING id INTO tid;
        INSERT INTO tariff_prices (tariff_id, price, valid_from, valid_to, comment)
            SELECT tid, price, valid_from, valid_to, comment FROM service_tariffs WHERE service_id = s.id AND deleted_at IS NULL;
        -- сначала убираем собственные цены: триггер не даёт услуге со ссылкой на тариф иметь свои цены
        UPDATE service_tariffs SET deleted_at = now(), updated_at = now() WHERE service_id = s.id AND deleted_at IS NULL;
        UPDATE services SET tariff_id = tid, updated_at = now() WHERE id = s.id;
    END LOOP;
END $$;

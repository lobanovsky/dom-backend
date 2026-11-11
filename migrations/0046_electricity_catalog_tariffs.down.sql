DROP TRIGGER electricity_tariff_guard ON tariffs;
DROP FUNCTION electricity_tariff_guard();
ALTER TABLE electricity_contracts DROP COLUMN tariff_t1_id, DROP COLUMN tariff_t2_id, DROP COLUMN tariff_t3_id;

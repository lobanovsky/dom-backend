DROP TABLE service_meters;
DROP TRIGGER meter_readings_check_period ON meter_readings;
DROP FUNCTION meter_readings_check_period();
DROP INDEX meter_readings_period_idx;
ALTER TABLE meter_readings DROP COLUMN period;

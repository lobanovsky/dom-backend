-- Расчётный месяц показаний и общедомовые счётчики услуги.
--
-- period - месяц, к которому относится показание (первое число месяца): показания октября берутся в расчёт за октябрь,
-- даже если сняты в других числах. По умолчанию - месяц даты показаний, но его можно задать явно (показание, снятое 28 сентября
-- для октябрьского расчёта). Расход за месяц = показание месяца - показание предыдущего месяца с показаниями.
ALTER TABLE meter_readings ADD COLUMN period DATE;
UPDATE meter_readings SET period = date_trunc('month', reading_date)::date;
ALTER TABLE meter_readings ALTER COLUMN period SET NOT NULL;
ALTER TABLE meter_readings ADD CONSTRAINT meter_readings_period_check CHECK (period = date_trunc('month', period)::date);
CREATE INDEX meter_readings_period_idx ON meter_readings (meter_id, period);

-- Один месяц - одно показание счётчика. Триггером, а не уникальным индексом: уже загруженные данные не трогаем
-- (если в одном месяце их несколько, расчёт берёт последнее по дате), а новые и правленые записи проверяются.
CREATE FUNCTION meter_readings_check_period() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL AND EXISTS (
        SELECT 1 FROM meter_readings r
        WHERE r.meter_id = NEW.meter_id AND r.period = NEW.period AND r.deleted_at IS NULL AND r.id <> NEW.id) THEN
        RAISE EXCEPTION 'a reading for this period already exists'
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'meter_readings_meter_period_key';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
CREATE TRIGGER meter_readings_check_period BEFORE INSERT OR UPDATE ON meter_readings
    FOR EACH ROW EXECUTE FUNCTION meter_readings_check_period();

-- Какие общедомовые счётчики берёт услуга calc_method = 'common_meter' (в доме их может быть несколько:
-- отдельный счётчик на машиноместа). Расходы выбранных счётчиков дома складываются и делятся между помещениями по площади.
CREATE TABLE service_meters (
    service_id  BIGINT NOT NULL REFERENCES services (id),
    meter_id    BIGINT NOT NULL REFERENCES meters (id),
    PRIMARY KEY (service_id, meter_id)
);
CREATE INDEX service_meters_meter_id_idx ON service_meters (meter_id);

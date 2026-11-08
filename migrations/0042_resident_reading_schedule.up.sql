-- Регулярное расписание и исключения по месяцам. Старую таблицу сохраняем для совместимости.
CREATE TABLE resident_reading_schedules (
 building_id BIGINT PRIMARY KEY REFERENCES buildings(id),
 enabled BOOLEAN NOT NULL DEFAULT false,
 opens_day INT NOT NULL DEFAULT 17 CHECK(opens_day BETWEEN 1 AND 31),
 closes_day INT NOT NULL DEFAULT 22 CHECK(closes_day BETWEEN 1 AND 31 AND closes_day>=opens_day),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE resident_reading_overrides (
 building_id BIGINT NOT NULL REFERENCES buildings(id),
 period DATE NOT NULL CHECK(extract(day FROM period)=1),
 opens_on DATE NOT NULL,
 closes_on DATE NOT NULL CHECK(closes_on>=opens_on),
 enabled BOOLEAN NOT NULL DEFAULT true,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(building_id,period)
);
INSERT INTO resident_reading_overrides(building_id,period,opens_on,closes_on)
SELECT building_id,period,opens_on,closes_on FROM resident_reading_windows;

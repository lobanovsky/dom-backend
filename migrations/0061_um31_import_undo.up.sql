ALTER TABLE meter_readings
    ADD COLUMN import_run_id BIGINT REFERENCES um31_import_runs(id);

ALTER TABLE um31_import_runs
    ADD COLUMN reading_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN undone_at TIMESTAMPTZ;

-- Успешные импорты до этой миграции выполнялись одной транзакцией, поэтому
-- созданные показания и строка истории имеют одинаковый created_at. Связываем
-- только однозначные совпадения с тем же домом, месяцем и количеством записей.
WITH candidates AS (
    SELECT i.id AS run_id,
           r.id AS reading_id,
           (i.result->>'readings')::INTEGER AS expected_count,
           count(*) OVER (PARTITION BY r.id) AS reading_runs
    FROM um31_import_runs i
    JOIN meter_readings r ON r.period=i.period AND r.created_at=i.created_at AND r.updated_at=r.created_at
        AND r.deleted_at IS NULL AND r.source='import'
    JOIN meters m ON m.id=r.meter_id AND m.resource='electricity'
    LEFT JOIN premises p ON p.id=m.premises_id
    WHERE m.building_id=i.building_id OR p.building_id=i.building_id
), safe_runs AS (
    SELECT run_id, expected_count
    FROM candidates
    GROUP BY run_id, expected_count
    HAVING count(*)=expected_count AND count(*) FILTER (WHERE reading_runs=1)=expected_count
), safe_readings AS (
    SELECT c.run_id, c.reading_id
    FROM candidates c
    JOIN safe_runs s ON s.run_id=c.run_id AND s.expected_count=c.expected_count
    WHERE c.reading_runs=1
)
UPDATE meter_readings r SET import_run_id=s.run_id
FROM safe_readings s WHERE r.id=s.reading_id;

UPDATE um31_import_runs i SET reading_count=counts.count
FROM (
    SELECT import_run_id, count(*)::INTEGER AS count
    FROM meter_readings WHERE import_run_id IS NOT NULL
    GROUP BY import_run_id
) counts WHERE i.id=counts.import_run_id;

CREATE INDEX meter_readings_import_run_idx
    ON meter_readings (import_run_id) WHERE import_run_id IS NOT NULL;

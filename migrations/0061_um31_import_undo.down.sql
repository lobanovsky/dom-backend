DROP INDEX meter_readings_import_run_idx;
ALTER TABLE um31_import_runs DROP COLUMN undone_at, DROP COLUMN reading_count;
ALTER TABLE meter_readings DROP COLUMN import_run_id;

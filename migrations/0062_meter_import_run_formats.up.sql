ALTER TABLE um31_import_runs RENAME TO meter_reading_import_runs;
ALTER INDEX um31_import_runs_building_created_idx RENAME TO meter_reading_import_runs_building_created_idx;

ALTER TABLE meter_reading_import_runs
    ADD COLUMN resource TEXT NOT NULL DEFAULT 'electricity',
    ADD COLUMN import_format TEXT NOT NULL DEFAULT 'um31';

ALTER INDEX meter_readings_import_run_idx RENAME TO meter_readings_import_run_id_idx;

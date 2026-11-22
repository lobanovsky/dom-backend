CREATE TABLE um31_import_runs (
    id BIGSERIAL PRIMARY KEY,
    building_id BIGINT NOT NULL REFERENCES buildings(id),
    file_name TEXT NOT NULL,
    period DATE NOT NULL,
    result JSONB NOT NULL,
    issues JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX um31_import_runs_building_created_idx
    ON um31_import_runs (building_id, created_at DESC);

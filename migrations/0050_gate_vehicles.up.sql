-- IF NOT EXISTS позволяет предварительно подготовить колонку при переносе данных.
ALTER TABLE gate_requests ADD COLUMN IF NOT EXISTS vehicles JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE gate_requests ADD CONSTRAINT gate_requests_vehicles_array CHECK(jsonb_typeof(vehicles)='array');
UPDATE gate_requests SET vehicles=jsonb_build_array(jsonb_build_object('plate_number',vehicle_plate,'brand',vehicle_brand,'active',true))
 WHERE vehicles='[]'::jsonb AND (vehicle_plate<>'' OR vehicle_brand<>'');

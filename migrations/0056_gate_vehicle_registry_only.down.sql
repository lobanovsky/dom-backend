ALTER TABLE gate_vehicles ADD COLUMN active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE gate_vehicles ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE gate_vehicles DROP CONSTRAINT gate_vehicles_person_id_fkey;
ALTER TABLE gate_vehicles ADD CONSTRAINT gate_vehicles_person_id_fkey FOREIGN KEY(person_id) REFERENCES persons(id);
ALTER TABLE gate_request_vehicles DROP CONSTRAINT gate_request_vehicles_vehicle_id_fkey;
ALTER TABLE gate_request_vehicles ADD CONSTRAINT gate_request_vehicles_vehicle_id_fkey FOREIGN KEY(vehicle_id) REFERENCES gate_vehicles(id);
DROP INDEX gate_vehicles_person_plate_uq;
CREATE UNIQUE INDEX gate_vehicles_person_plate_uq ON gate_vehicles(person_id,plate_key) WHERE plate_key<>'' AND deleted_at IS NULL;
CREATE INDEX gate_vehicles_person_active_idx ON gate_vehicles(person_id,active) WHERE deleted_at IS NULL;

ALTER TABLE gate_requests ADD COLUMN vehicles JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE gate_requests ADD CONSTRAINT gate_requests_vehicles_array CHECK(jsonb_typeof(vehicles)='array');
ALTER TABLE gate_requests ADD COLUMN vehicle_plate TEXT NOT NULL DEFAULT '';
ALTER TABLE gate_requests ADD COLUMN vehicle_brand TEXT NOT NULL DEFAULT '';
UPDATE gate_requests g SET vehicles=COALESCE((
 SELECT jsonb_agg(jsonb_build_object('plate_number',v.plate_number,'brand',v.brand,'active',true) ORDER BY v.id)
 FROM gate_request_vehicles rv JOIN gate_vehicles v ON v.id=rv.vehicle_id WHERE rv.request_id=g.id
),'[]'::jsonb),
vehicle_plate=COALESCE((SELECT v.plate_number FROM gate_request_vehicles rv JOIN gate_vehicles v ON v.id=rv.vehicle_id WHERE rv.request_id=g.id ORDER BY v.id LIMIT 1),''),
vehicle_brand=COALESCE((SELECT v.brand FROM gate_request_vehicles rv JOIN gate_vehicles v ON v.id=rv.vehicle_id WHERE rv.request_id=g.id ORDER BY v.id LIMIT 1),'');

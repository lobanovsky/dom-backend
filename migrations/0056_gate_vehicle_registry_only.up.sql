-- Архивные автомобили больше не используются и очищаются вместе со связями.
ALTER TABLE gate_request_vehicles DROP CONSTRAINT gate_request_vehicles_vehicle_id_fkey;
ALTER TABLE gate_request_vehicles ADD CONSTRAINT gate_request_vehicles_vehicle_id_fkey
 FOREIGN KEY(vehicle_id) REFERENCES gate_vehicles(id) ON DELETE CASCADE;
ALTER TABLE gate_vehicles DROP CONSTRAINT gate_vehicles_person_id_fkey;
ALTER TABLE gate_vehicles ADD CONSTRAINT gate_vehicles_person_id_fkey
 FOREIGN KEY(person_id) REFERENCES persons(id) ON DELETE CASCADE;
DELETE FROM gate_vehicles WHERE NOT active OR deleted_at IS NOT NULL;

DROP INDEX gate_vehicles_person_plate_uq;
DROP INDEX gate_vehicles_person_active_idx;
CREATE UNIQUE INDEX gate_vehicles_person_plate_uq ON gate_vehicles(person_id,plate_key) WHERE plate_key<>'';
ALTER TABLE gate_vehicles DROP COLUMN active, DROP COLUMN deleted_at;

ALTER TABLE gate_requests DROP CONSTRAINT gate_requests_vehicles_array;
ALTER TABLE gate_requests DROP COLUMN vehicles, DROP COLUMN vehicle_plate, DROP COLUMN vehicle_brand;

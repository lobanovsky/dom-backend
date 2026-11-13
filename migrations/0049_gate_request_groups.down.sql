ALTER TABLE gate_requests DROP COLUMN no_personal_vehicle;
ALTER TABLE gate_requests DROP COLUMN group_id;
ALTER TABLE gate_requests DROP CONSTRAINT gate_requests_group_owner_key;

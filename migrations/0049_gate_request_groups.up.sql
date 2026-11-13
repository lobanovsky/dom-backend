ALTER TABLE gate_requests ADD COLUMN group_id BIGINT;
UPDATE gate_requests SET group_id=id;
ALTER TABLE gate_requests ALTER COLUMN group_id SET NOT NULL;
ALTER TABLE gate_requests ADD CONSTRAINT gate_requests_group_owner_key UNIQUE(id,person_id);
ALTER TABLE gate_requests ADD CONSTRAINT gate_requests_group_fkey FOREIGN KEY(group_id,person_id) REFERENCES gate_requests(id,person_id);
CREATE INDEX gate_requests_group_idx ON gate_requests(group_id,id);
ALTER TABLE gate_requests ADD COLUMN no_personal_vehicle BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK(role IN('admin','viewer','guard'));
CREATE TABLE gate_requests (
 id BIGSERIAL PRIMARY KEY,
 person_id BIGINT NOT NULL REFERENCES persons(id),
 ownership_id BIGINT NOT NULL REFERENCES ownerships(id),
 premises_id BIGINT NOT NULL REFERENCES premises(id),
 zone TEXT NOT NULL CHECK(zone IN('courtyard','parking')),
 phone TEXT NOT NULL CHECK(phone ~ '^[1-9][0-9]{10,14}$'),
 recipient_name TEXT NOT NULL CHECK(length(trim(recipient_name)) BETWEEN 1 AND 120),
 relation TEXT NOT NULL CHECK(relation IN('owner','relative','tenant','guest','service')),
 vehicle_plate TEXT NOT NULL DEFAULT '' CHECK(length(vehicle_plate)<=24),
 vehicle_brand TEXT NOT NULL DEFAULT '' CHECK(length(vehicle_brand)<=80),
 occasional_vehicle BOOLEAN NOT NULL DEFAULT false,
 valid_until DATE,
 comment TEXT NOT NULL DEFAULT '' CHECK(length(comment)<=500),
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN('pending','approved','rejected','cancelled','revoked')),
 review_note TEXT NOT NULL DEFAULT '' CHECK(length(review_note)<=500),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX gate_requests_person_idx ON gate_requests(person_id,id DESC);
CREATE INDEX gate_requests_status_idx ON gate_requests(status,id DESC);
CREATE INDEX gate_requests_premises_phone_idx ON gate_requests(premises_id,zone,phone);
CREATE TABLE gate_events (
 id BIGSERIAL PRIMARY KEY,
 request_id BIGINT NOT NULL REFERENCES gate_requests(id) ON DELETE CASCADE,
 action TEXT NOT NULL CHECK(action IN('submitted','approved','rejected','cancelled','revoked')),
 actor_role TEXT NOT NULL CHECK(actor_role IN('resident','admin','guard')),
 actor_user_id BIGINT REFERENCES users(id),
 actor_person_id BIGINT REFERENCES persons(id),
 note TEXT NOT NULL DEFAULT '' CHECK(length(note)<=500),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK((actor_role='resident' AND actor_person_id IS NOT NULL AND actor_user_id IS NULL)
 OR (actor_role IN('admin','guard') AND actor_user_id IS NOT NULL AND actor_person_id IS NULL))
);
CREATE INDEX gate_events_request_idx ON gate_events(request_id,id);

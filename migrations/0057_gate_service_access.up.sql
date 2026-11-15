CREATE TABLE gate_service_accesses (
 id BIGSERIAL PRIMARY KEY,
 category TEXT NOT NULL CHECK(category IN ('staff','contractor','special_vehicle')),
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 120),
 phone TEXT NOT NULL CHECK(phone ~ '^[0-9]{10,15}$'),
 vehicle_plate TEXT NOT NULL DEFAULT '',
 vehicle_brand TEXT NOT NULL DEFAULT '',
 valid_until DATE,
 comment TEXT NOT NULL DEFAULT '',
 created_by BIGINT REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE gate_service_access_zones (
 id BIGSERIAL PRIMARY KEY,
 access_id BIGINT NOT NULL REFERENCES gate_service_accesses(id) ON DELETE CASCADE,
 zone TEXT NOT NULL CHECK(zone IN ('courtyard','parking')),
 status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','revoke_pending','revoked','expired')),
 pending_action TEXT NOT NULL DEFAULT '' CHECK(pending_action IN ('','revoke','expire')),
 sync_error TEXT NOT NULL DEFAULT '',
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(access_id,zone)
);
CREATE INDEX gate_service_access_zone_status_idx ON gate_service_access_zones(zone,status,access_id);
CREATE INDEX gate_service_access_expiry_idx ON gate_service_access_zones(status,access_id) WHERE status IN ('active','revoke_pending');
CREATE TABLE gate_service_access_events (
 id BIGSERIAL PRIMARY KEY,
 access_id BIGINT NOT NULL REFERENCES gate_service_accesses(id) ON DELETE CASCADE,
 zone TEXT CHECK(zone IN ('courtyard','parking')),
 action TEXT NOT NULL,
 actor_user_id BIGINT REFERENCES users(id),
 note TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

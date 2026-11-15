ALTER TABLE gate_requests DROP CONSTRAINT gate_requests_status_check;
ALTER TABLE gate_requests ADD CONSTRAINT gate_requests_status_check
 CHECK(status IN('pending','approved','rejected','cancelled','revoked','revoke_requested'));

ALTER TABLE gate_events DROP CONSTRAINT gate_events_action_check;
ALTER TABLE gate_events ADD CONSTRAINT gate_events_action_check
 CHECK(action IN('submitted','approved','rejected','cancelled','revoked','updated','created','revoke_requested'));

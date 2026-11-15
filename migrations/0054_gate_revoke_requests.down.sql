DELETE FROM gate_events WHERE action='revoke_requested';
UPDATE gate_requests SET status='revoked' WHERE status='revoke_requested';

ALTER TABLE gate_requests DROP CONSTRAINT gate_requests_status_check;
ALTER TABLE gate_requests ADD CONSTRAINT gate_requests_status_check
 CHECK(status IN('pending','approved','rejected','cancelled','revoked'));

ALTER TABLE gate_events DROP CONSTRAINT gate_events_action_check;
ALTER TABLE gate_events ADD CONSTRAINT gate_events_action_check
 CHECK(action IN('submitted','approved','rejected','cancelled','revoked','updated','created'));

ALTER TABLE gate_events DROP CONSTRAINT gate_events_action_check;
ALTER TABLE gate_events ADD CONSTRAINT gate_events_action_check
 CHECK(action IN('submitted','approved','rejected','cancelled','revoked','updated'));

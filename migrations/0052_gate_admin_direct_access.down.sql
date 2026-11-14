DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM gate_events WHERE action='created') THEN
  RAISE EXCEPTION 'cannot roll back direct gate access while its history exists';
 END IF;
END $$;
ALTER TABLE gate_events DROP CONSTRAINT gate_events_action_check;
ALTER TABLE gate_events ADD CONSTRAINT gate_events_action_check
 CHECK(action IN('submitted','approved','rejected','cancelled','revoked','updated'));

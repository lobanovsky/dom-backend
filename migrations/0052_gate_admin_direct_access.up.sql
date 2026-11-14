ALTER TABLE gate_events DROP CONSTRAINT gate_events_action_check;
ALTER TABLE gate_events ADD CONSTRAINT gate_events_action_check
 CHECK(action IN('submitted','approved','rejected','cancelled','revoked','updated','created'));

UPDATE gate_requests AS g
SET vehicle_plate=lower(g.vehicle_plate),
    vehicles=COALESCE((
      SELECT jsonb_agg(
        jsonb_set(car.value,'{plate_number}',to_jsonb(lower(COALESCE(car.value->>'plate_number',''))),true)
        ORDER BY car.ordinality
      )
      FROM jsonb_array_elements(g.vehicles) WITH ORDINALITY AS car(value,ordinality)
    ),'[]'::jsonb);

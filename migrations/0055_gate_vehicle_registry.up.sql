CREATE TABLE gate_vehicles (
 id BIGSERIAL PRIMARY KEY,
 person_id BIGINT NOT NULL REFERENCES persons(id),
 plate_number TEXT NOT NULL DEFAULT '',
 plate_key TEXT NOT NULL DEFAULT '',
 brand TEXT NOT NULL DEFAULT '',
 active BOOLEAN NOT NULL DEFAULT TRUE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 deleted_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX gate_vehicles_person_plate_uq ON gate_vehicles(person_id,plate_key) WHERE plate_key<>'' AND deleted_at IS NULL;
CREATE INDEX gate_vehicles_person_active_idx ON gate_vehicles(person_id,active) WHERE deleted_at IS NULL;
CREATE TABLE gate_request_vehicles (
 request_id BIGINT NOT NULL REFERENCES gate_requests(id) ON DELETE CASCADE,
 vehicle_id BIGINT NOT NULL REFERENCES gate_vehicles(id),
 PRIMARY KEY(request_id,vehicle_id)
);

-- Переносим автомобили из JSON каждой заявки в реестр владельца. Госномера одного
-- собственника объединяются независимо от пробелов, дефисов и регистра.
DO $$
DECLARE r RECORD; c RECORD; v_id BIGINT; v_key TEXT;
BEGIN
 FOR r IN SELECT id,person_id,vehicles,vehicle_plate,vehicle_brand FROM gate_requests ORDER BY id LOOP
  IF jsonb_array_length(COALESCE(r.vehicles,'[]'::jsonb))=0 AND (r.vehicle_plate<>'' OR r.vehicle_brand<>'') THEN
   r.vehicles:=jsonb_build_array(jsonb_build_object('plate_number',r.vehicle_plate,'brand',r.vehicle_brand,'active',true));
  END IF;
  FOR c IN SELECT value FROM jsonb_array_elements(COALESCE(r.vehicles,'[]'::jsonb)) LOOP
   v_key:=regexp_replace(upper(COALESCE(c.value->>'plate_number','')),'[[:space:]-]','','g');
   v_id:=NULL;
   IF v_key<>'' THEN
    SELECT id INTO v_id FROM gate_vehicles WHERE person_id=r.person_id AND plate_key=v_key AND deleted_at IS NULL LIMIT 1;
   END IF;
   IF v_id IS NULL THEN
    INSERT INTO gate_vehicles(person_id,plate_number,plate_key,brand,active)
    VALUES(r.person_id,lower(trim(COALESCE(c.value->>'plate_number',''))),v_key,trim(COALESCE(c.value->>'brand','')),COALESCE((c.value->>'active')::boolean,true)) RETURNING id INTO v_id;
   END IF;
   INSERT INTO gate_request_vehicles(request_id,vehicle_id) VALUES(r.id,v_id) ON CONFLICT DO NOTHING;
  END LOOP;
 END LOOP;
END $$;

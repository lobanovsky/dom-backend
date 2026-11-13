CREATE TABLE water_contracts (
 id BIGSERIAL PRIMARY KEY,
 name TEXT NOT NULL UNIQUE CHECK (length(trim(name)) BETWEEN 1 AND 200),
 number TEXT CHECK (length(number)<=200),
 water_tariff_id BIGINT NOT NULL REFERENCES tariffs(id),
 sewer_tariff_id BIGINT NOT NULL REFERENCES tariffs(id),
 archived_at DATE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE water_contract_meters (
 contract_id BIGINT NOT NULL REFERENCES water_contracts(id) ON DELETE CASCADE,
 meter_id BIGINT NOT NULL UNIQUE REFERENCES meters(id),
 PRIMARY KEY(contract_id,meter_id)
);

-- Изменение справочника не должно нарушать уже настроенные договоры.
CREATE FUNCTION water_tariff_guard() RETURNS trigger AS $$
BEGIN
 IF (NEW.unit IS DISTINCT FROM OLD.unit OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR (NEW.deleted_at IS NOT NULL AND OLD.deleted_at IS NULL))
 AND EXISTS(SELECT 1 FROM water_contracts WHERE OLD.id IN(water_tariff_id,sewer_tariff_id)) THEN
  RAISE EXCEPTION 'tariff is used by water contracts';
 END IF;
 RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER water_tariff_guard BEFORE UPDATE ON tariffs FOR EACH ROW EXECUTE FUNCTION water_tariff_guard();

CREATE FUNCTION water_meter_guard() RETURNS trigger AS $$
BEGIN
 IF (NEW.resource IS DISTINCT FROM OLD.resource OR NEW.building_id IS DISTINCT FROM OLD.building_id
     OR NEW.premises_id IS DISTINCT FROM OLD.premises_id
     OR (NEW.deleted_at IS NOT NULL AND OLD.deleted_at IS NULL))
 AND EXISTS(SELECT 1 FROM water_contract_meters WHERE meter_id=OLD.id) THEN
  RAISE EXCEPTION 'meter is used by water contracts';
 END IF;
 RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER water_meter_guard BEFORE UPDATE ON meters FOR EACH ROW EXECUTE FUNCTION water_meter_guard();

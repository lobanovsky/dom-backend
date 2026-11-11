-- Ссылки на справочник сохраняют историю цен отдельно от настроек договоров.
ALTER TABLE electricity_contracts
 ADD COLUMN tariff_t1_id BIGINT REFERENCES tariffs(id),
 ADD COLUMN tariff_t2_id BIGINT REFERENCES tariffs(id),
 ADD COLUMN tariff_t3_id BIGINT REFERENCES tariffs(id);

CREATE FUNCTION electricity_tariff_guard() RETURNS trigger AS $$
BEGIN
 IF (NEW.unit IS DISTINCT FROM OLD.unit OR NEW.organization_id IS DISTINCT FROM OLD.organization_id
     OR (NEW.deleted_at IS NOT NULL AND OLD.deleted_at IS NULL))
 AND EXISTS(SELECT 1 FROM electricity_contracts c WHERE OLD.id IN (c.tariff_t1_id,c.tariff_t2_id,c.tariff_t3_id)) THEN
   RAISE EXCEPTION 'tariff is used by electricity contracts';
 END IF;
 RETURN NEW;
END
$$ LANGUAGE plpgsql;
CREATE TRIGGER electricity_tariff_guard BEFORE UPDATE ON tariffs
 FOR EACH ROW EXECUTE FUNCTION electricity_tariff_guard();

-- Существующие общие контакты сохраняем. Новое назначение адреса кабинета проверяется атомарно.
CREATE FUNCTION check_person_resident_email() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE selected_email text;
BEGIN
 IF NEW.deleted_at IS NOT NULL THEN RETURN NEW; END IF;
 selected_email := lower(btrim(NEW.emails[1]));
 IF selected_email IS NULL OR selected_email = '' THEN RETURN NEW; END IF;
 IF TG_OP = 'UPDATE' THEN
  IF OLD.deleted_at IS NULL AND lower(btrim(OLD.emails[1])) IS NOT DISTINCT FROM selected_email THEN RETURN NEW; END IF;
 END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended('person-resident-email:' || selected_email, 0));
 IF EXISTS(SELECT 1 FROM persons p WHERE p.id <> NEW.id AND p.deleted_at IS NULL AND lower(btrim(p.emails[1]))=selected_email)
 OR EXISTS(SELECT 1 FROM resident_accounts a WHERE a.person_id <> NEW.id AND a.email=selected_email)
 OR EXISTS(SELECT 1 FROM resident_access a WHERE a.person_id <> NEW.id AND a.enabled AND a.email=selected_email) THEN
  RAISE EXCEPTION 'emails: resident email is already selected for another person';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER persons_resident_email BEFORE INSERT OR UPDATE OF emails,deleted_at ON persons
FOR EACH ROW EXECUTE FUNCTION check_person_resident_email();

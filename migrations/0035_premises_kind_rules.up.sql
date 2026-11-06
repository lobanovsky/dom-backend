-- Правила вида помещения: у машиноместа один собственник и не бывает проживающих. Признаки хранятся в
-- справочнике видов (premises_kinds), а не зашиты в код; нулевое значение = прежнее поведение.
ALTER TABLE premises_kinds
    ADD COLUMN no_residents BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN single_owner BOOLEAN NOT NULL DEFAULT false;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM residencies r JOIN premises p ON p.id = r.premises_id
               WHERE p.kind = 'parking_space' AND r.deleted_at IS NULL AND p.deleted_at IS NULL) THEN
        RAISE EXCEPTION 'parking spaces have residents: fix the data first';
    END IF;
    IF EXISTS (SELECT 1 FROM ownerships a JOIN ownerships b
                 ON a.premises_id = b.premises_id AND a.id < b.id AND a.deleted_at IS NULL AND b.deleted_at IS NULL
                    AND daterange(a.valid_from, a.valid_to, '[]') && daterange(b.valid_from, b.valid_to, '[]')
               JOIN premises p ON p.id = a.premises_id WHERE p.kind = 'parking_space') THEN
        RAISE EXCEPTION 'parking spaces have several owners at once: fix the data first';
    END IF;
END $$;

UPDATE premises_kinds SET no_residents = true, single_owner = true WHERE code = 'parking_space';

-- Проживание нельзя завести (или восстановить) в помещении вида «без проживающих».
CREATE FUNCTION residencies_check_premises_kind() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL AND (TG_OP = 'INSERT' OR NEW.premises_id IS DISTINCT FROM OLD.premises_id OR OLD.deleted_at IS NOT NULL) THEN
        IF EXISTS (SELECT 1 FROM premises p JOIN premises_kinds k ON k.code = p.kind AND k.deleted_at IS NULL
                   WHERE p.id = NEW.premises_id AND k.no_residents) THEN
            RAISE EXCEPTION 'cannot save: premises kind does not allow residents';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER residencies_check_premises_kind BEFORE INSERT OR UPDATE ON residencies
    FOR EACH ROW EXECUTE FUNCTION residencies_check_premises_kind();

-- В помещении вида «один собственник» периоды действующих владений не пересекаются.
CREATE FUNCTION ownerships_check_single_owner() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL AND (TG_OP = 'INSERT' OR NEW.premises_id IS DISTINCT FROM OLD.premises_id
       OR NEW.valid_from IS DISTINCT FROM OLD.valid_from OR NEW.valid_to IS DISTINCT FROM OLD.valid_to
       OR OLD.deleted_at IS NOT NULL) THEN
        IF EXISTS (SELECT 1 FROM premises p JOIN premises_kinds k ON k.code = p.kind AND k.deleted_at IS NULL
                   WHERE p.id = NEW.premises_id AND k.single_owner)
           AND EXISTS (SELECT 1 FROM ownerships o
                       WHERE o.premises_id = NEW.premises_id AND o.id <> NEW.id AND o.deleted_at IS NULL
                         AND daterange(o.valid_from, o.valid_to, '[]') && daterange(NEW.valid_from, NEW.valid_to, '[]')) THEN
            RAISE EXCEPTION 'cannot save: premises kind allows a single owner';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER ownerships_check_single_owner BEFORE INSERT OR UPDATE ON ownerships
    FOR EACH ROW EXECUTE FUNCTION ownerships_check_single_owner();

-- Помещение нельзя перевести в вид, правилам которого уже не отвечают его проживающие и собственники.
CREATE FUNCTION premises_check_kind_rules() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL AND NEW.kind IS DISTINCT FROM OLD.kind THEN
        IF EXISTS (SELECT 1 FROM premises_kinds k WHERE k.code = NEW.kind AND k.deleted_at IS NULL AND k.no_residents)
           AND EXISTS (SELECT 1 FROM residencies r WHERE r.premises_id = NEW.id AND r.deleted_at IS NULL) THEN
            RAISE EXCEPTION 'cannot save: premises has residents and the kind does not allow them';
        END IF;
        IF EXISTS (SELECT 1 FROM premises_kinds k WHERE k.code = NEW.kind AND k.deleted_at IS NULL AND k.single_owner)
           AND EXISTS (SELECT 1 FROM ownerships a JOIN ownerships b ON a.premises_id = b.premises_id AND a.id < b.id
                       AND a.deleted_at IS NULL AND b.deleted_at IS NULL
                       AND daterange(a.valid_from, a.valid_to, '[]') && daterange(b.valid_from, b.valid_to, '[]')
                       WHERE a.premises_id = NEW.id) THEN
            RAISE EXCEPTION 'cannot save: premises has several owners and the kind allows a single owner';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER premises_check_kind_rules BEFORE UPDATE ON premises
    FOR EACH ROW EXECUTE FUNCTION premises_check_kind_rules();

-- Правило нельзя включить для вида, помещения которого ему уже не отвечают.
CREATE FUNCTION premises_kinds_check_rules() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL AND NEW.no_residents AND NOT OLD.no_residents THEN
        IF EXISTS (SELECT 1 FROM residencies r JOIN premises p ON p.id = r.premises_id
                   WHERE p.kind = NEW.code AND p.deleted_at IS NULL AND r.deleted_at IS NULL) THEN
            RAISE EXCEPTION 'cannot save: kind has premises with residents';
        END IF;
    END IF;
    IF NEW.deleted_at IS NULL AND NEW.single_owner AND NOT OLD.single_owner THEN
        IF EXISTS (SELECT 1 FROM ownerships a JOIN ownerships b ON a.premises_id = b.premises_id AND a.id < b.id
                       AND a.deleted_at IS NULL AND b.deleted_at IS NULL
                       AND daterange(a.valid_from, a.valid_to, '[]') && daterange(b.valid_from, b.valid_to, '[]')
                   JOIN premises p ON p.id = a.premises_id WHERE p.kind = NEW.code AND p.deleted_at IS NULL) THEN
            RAISE EXCEPTION 'cannot save: kind has premises with several owners';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER premises_kinds_check_rules BEFORE UPDATE ON premises_kinds
    FOR EACH ROW EXECUTE FUNCTION premises_kinds_check_rules();

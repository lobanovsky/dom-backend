-- Мягкое удаление: запись не удаляется физически, а получает deleted_at.
-- Это сохраняет историю и позволяет восстановить случайно удалённое.
ALTER TABLE organizations      ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE buildings          ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE premises           ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE persons            ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE legal_entities     ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE ownerships         ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE residencies        ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE personal_accounts  ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE account_holders    ADD COLUMN deleted_at TIMESTAMPTZ;

-- Уникальность действует только среди неудалённых записей: номер помещения,
-- лицевого счёта и т.п. можно использовать снова. Имена индексов сохраняем:
-- по ним фронтенд подбирает текст ошибки о дубликате.
ALTER TABLE premises DROP CONSTRAINT premises_building_id_kind_number_key;
CREATE UNIQUE INDEX premises_building_id_kind_number_key ON premises (building_id, kind, number) WHERE deleted_at IS NULL;

ALTER TABLE personal_accounts DROP CONSTRAINT personal_accounts_number_key;
CREATE UNIQUE INDEX personal_accounts_number_key ON personal_accounts (number) WHERE deleted_at IS NULL;

DROP INDEX premises_cadastral_number_uq;
CREATE UNIQUE INDEX premises_cadastral_number_uq ON premises (cadastral_number) WHERE cadastral_number IS NOT NULL AND deleted_at IS NULL;

DROP INDEX buildings_cadastral_number_uq;
CREATE UNIQUE INDEX buildings_cadastral_number_uq ON buildings (cadastral_number) WHERE cadastral_number IS NOT NULL AND deleted_at IS NULL;

DROP INDEX legal_entities_inn_kpp_uq;
CREATE UNIQUE INDEX legal_entities_inn_kpp_uq ON legal_entities (inn, COALESCE(kpp, '')) WHERE deleted_at IS NULL;

-- Целостность между живыми и удалёнными записями. Правила в БД, а не в коде,
-- чтобы они действовали на любом пути: создание, правка, удаление, восстановление.

-- Запись нельзя сохранить (или восстановить), если она ссылается на удалённую.
CREATE FUNCTION assert_active(tbl regclass, ref bigint, what text) RETURNS void AS $$
DECLARE
    is_deleted boolean;
BEGIN
    IF ref IS NULL THEN
        RETURN;
    END IF;
    EXECUTE format('SELECT deleted_at IS NOT NULL FROM %s WHERE id = $1', tbl) INTO is_deleted USING ref;
    IF is_deleted THEN
        RAISE EXCEPTION 'cannot save: % is deleted', what;
    END IF;
END
$$ LANGUAGE plpgsql;

-- Запись нельзя удалить, пока на неё ссылаются неудалённые.
CREATE FUNCTION assert_no_active(tbl regclass, col text, ref bigint, what text) RETURNS void AS $$
DECLARE
    has_active boolean;
BEGIN
    EXECUTE format('SELECT EXISTS (SELECT 1 FROM %s WHERE %I = $1 AND deleted_at IS NULL)', tbl, col) INTO has_active USING ref;
    IF has_active THEN
        RAISE EXCEPTION 'cannot delete: has active %', what;
    END IF;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION buildings_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('organizations', NEW.organization_id, 'organization');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION premises_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('buildings', NEW.building_id, 'building');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION ownerships_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('premises', NEW.premises_id, 'premises');
        PERFORM assert_active('persons', NEW.person_id, 'person');
        PERFORM assert_active('legal_entities', NEW.legal_entity_id, 'legal entity');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION residencies_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('premises', NEW.premises_id, 'premises');
        PERFORM assert_active('persons', NEW.person_id, 'person');
        PERFORM assert_active('persons', NEW.related_owner_id, 'related owner');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION personal_accounts_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('premises', NEW.premises_id, 'premises');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION account_holders_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('personal_accounts', NEW.account_id, 'account');
        PERFORM assert_active('persons', NEW.person_id, 'person');
        PERFORM assert_active('legal_entities', NEW.legal_entity_id, 'legal entity');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER buildings_check_parents BEFORE INSERT OR UPDATE ON buildings
    FOR EACH ROW EXECUTE FUNCTION buildings_check_parents();
CREATE TRIGGER premises_check_parents BEFORE INSERT OR UPDATE ON premises
    FOR EACH ROW EXECUTE FUNCTION premises_check_parents();
CREATE TRIGGER ownerships_check_parents BEFORE INSERT OR UPDATE ON ownerships
    FOR EACH ROW EXECUTE FUNCTION ownerships_check_parents();
CREATE TRIGGER residencies_check_parents BEFORE INSERT OR UPDATE ON residencies
    FOR EACH ROW EXECUTE FUNCTION residencies_check_parents();
CREATE TRIGGER personal_accounts_check_parents BEFORE INSERT OR UPDATE ON personal_accounts
    FOR EACH ROW EXECUTE FUNCTION personal_accounts_check_parents();
CREATE TRIGGER account_holders_check_parents BEFORE INSERT OR UPDATE ON account_holders
    FOR EACH ROW EXECUTE FUNCTION account_holders_check_parents();

CREATE FUNCTION organizations_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('buildings', 'organization_id', OLD.id, 'buildings');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION buildings_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('premises', 'building_id', OLD.id, 'premises');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION premises_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('ownerships', 'premises_id', OLD.id, 'ownerships');
    PERFORM assert_no_active('residencies', 'premises_id', OLD.id, 'residencies');
    PERFORM assert_no_active('personal_accounts', 'premises_id', OLD.id, 'accounts');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION persons_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('ownerships', 'person_id', OLD.id, 'ownerships');
    PERFORM assert_no_active('residencies', 'person_id', OLD.id, 'residencies');
    PERFORM assert_no_active('residencies', 'related_owner_id', OLD.id, 'residencies');
    PERFORM assert_no_active('account_holders', 'person_id', OLD.id, 'account holders');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION legal_entities_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('ownerships', 'legal_entity_id', OLD.id, 'ownerships');
    PERFORM assert_no_active('account_holders', 'legal_entity_id', OLD.id, 'account holders');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION personal_accounts_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('account_holders', 'account_id', OLD.id, 'account holders');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER organizations_check_children BEFORE UPDATE ON organizations
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION organizations_check_children();
CREATE TRIGGER buildings_check_children BEFORE UPDATE ON buildings
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION buildings_check_children();
CREATE TRIGGER premises_check_children BEFORE UPDATE ON premises
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION premises_check_children();
CREATE TRIGGER persons_check_children BEFORE UPDATE ON persons
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION persons_check_children();
CREATE TRIGGER legal_entities_check_children BEFORE UPDATE ON legal_entities
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION legal_entities_check_children();
CREATE TRIGGER personal_accounts_check_children BEFORE UPDATE ON personal_accounts
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION personal_accounts_check_children();

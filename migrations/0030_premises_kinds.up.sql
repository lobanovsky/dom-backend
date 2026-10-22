-- Справочник видов помещений: вместо жёсткого списка в CHECK. premises.kind и services.premises_kinds
-- по-прежнему хранят код вида (контракт API не меняется), а существование вида проверяют триггеры.
CREATE TABLE premises_kinds (
    id         BIGSERIAL PRIMARY KEY,
    code       TEXT        NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]{0,39}$'),
    name       TEXT        NOT NULL CHECK (btrim(name) <> ''),
    position   INTEGER     NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);
-- Код — идентификатор, на который ссылаются помещения, поэтому уникален и среди удалённых.
CREATE UNIQUE INDEX premises_kinds_code_uq ON premises_kinds (code);
CREATE UNIQUE INDEX premises_kinds_name_uq ON premises_kinds (lower(name)) WHERE deleted_at IS NULL;

INSERT INTO premises_kinds (code, name, position) VALUES
    ('apartment', 'Квартира', 10),
    ('non_residential', 'Нежилое помещение', 20),
    ('commercial', 'Коммерческое помещение', 30),
    ('parking_space', 'Машиноместо', 40),
    ('storage', 'Кладовая', 50);

ALTER TABLE premises DROP CONSTRAINT premises_kind_check;
ALTER TABLE services DROP CONSTRAINT services_premises_kinds_check;

-- Помещение (и услуга) могут ссылаться только на действующий вид.
CREATE FUNCTION premises_kind_exists(k text) RETURNS boolean AS $$
    SELECT EXISTS (SELECT 1 FROM premises_kinds WHERE code = k AND deleted_at IS NULL)
$$ LANGUAGE sql STABLE;

CREATE FUNCTION premises_check_kind() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL AND (TG_OP = 'INSERT' OR NEW.kind IS DISTINCT FROM OLD.kind OR OLD.deleted_at IS NOT NULL) THEN
        IF NOT premises_kind_exists(NEW.kind) THEN
            RAISE EXCEPTION 'cannot save: premises kind is unknown or deleted';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER premises_check_kind BEFORE INSERT OR UPDATE ON premises
    FOR EACH ROW EXECUTE FUNCTION premises_check_kind();

CREATE FUNCTION services_check_kinds() RETURNS trigger AS $$
DECLARE
    k text;
BEGIN
    IF NEW.deleted_at IS NULL AND (TG_OP = 'INSERT' OR NEW.premises_kinds IS DISTINCT FROM OLD.premises_kinds OR OLD.deleted_at IS NOT NULL) THEN
        FOREACH k IN ARRAY NEW.premises_kinds LOOP
            IF NOT premises_kind_exists(k) THEN
                RAISE EXCEPTION 'cannot save: premises kind is unknown or deleted';
            END IF;
        END LOOP;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER services_check_kinds BEFORE INSERT OR UPDATE ON services
    FOR EACH ROW EXECUTE FUNCTION services_check_kinds();

-- Вид нельзя удалить, пока им пользуются действующие помещения или услуги.
CREATE FUNCTION premises_kinds_check_children() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NOT NULL AND OLD.deleted_at IS NULL THEN
        IF EXISTS (SELECT 1 FROM premises WHERE kind = OLD.code AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'cannot delete: has active premises';
        END IF;
        IF EXISTS (SELECT 1 FROM services WHERE OLD.code = ANY (premises_kinds) AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'cannot delete: has active services';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER premises_kinds_check_children BEFORE UPDATE ON premises_kinds
    FOR EACH ROW EXECUTE FUNCTION premises_kinds_check_children();

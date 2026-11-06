-- Справочник родства проживающих с собственником: вместо жёсткого списка в CHECK. residencies.relation
-- по-прежнему хранит код (контракт API не меняется), а существование записи проверяет триггер.
CREATE TABLE kinships (
    id         BIGSERIAL PRIMARY KEY,
    code       TEXT        NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]{0,39}$'),
    name       TEXT        NOT NULL CHECK (btrim(name) <> ''),
    position   INTEGER     NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);
-- Код — идентификатор, на который ссылаются проживающие, поэтому уникален и среди удалённых.
CREATE UNIQUE INDEX kinships_code_uq ON kinships (code);
CREATE UNIQUE INDEX kinships_name_uq ON kinships (lower(name)) WHERE deleted_at IS NULL;

-- Прежние шесть кодов остаются (существующие записи не ломаются); остальные — из «Домовладельца».
INSERT INTO kinships (code, name, position) VALUES
    ('owner', 'Собственник', 5),
    ('spouse', 'Супруг(а)', 10),
    ('husband', 'Муж', 11),
    ('wife', 'Жена', 12),
    ('child', 'Ребёнок', 20),
    ('son', 'Сын', 21),
    ('daughter', 'Дочь', 22),
    ('parent', 'Родитель', 30),
    ('father', 'Отец', 31),
    ('mother', 'Мать', 32),
    ('grandson', 'Внук', 40),
    ('granddaughter', 'Внучка', 41),
    ('grandfather', 'Дедушка', 42),
    ('grandmother', 'Бабушка', 43),
    ('brother', 'Брат', 50),
    ('sister', 'Сестра', 51),
    ('guardian', 'Опекун', 60),
    ('relative', 'Родственник', 70),
    ('not_relative', 'Не родственник', 80),
    ('tenant', 'Арендатор', 90),
    ('temporary', 'Временно проживающий', 91),
    ('other', 'Другое', 100);

ALTER TABLE residencies DROP CONSTRAINT residencies_relation_check;

CREATE FUNCTION kinship_exists(k text) RETURNS boolean AS $$
    SELECT EXISTS (SELECT 1 FROM kinships WHERE code = k AND deleted_at IS NULL)
$$ LANGUAGE sql STABLE;

-- Проживание может ссылаться только на действующую запись справочника.
CREATE FUNCTION residencies_check_kinship() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL AND (TG_OP = 'INSERT' OR NEW.relation IS DISTINCT FROM OLD.relation OR OLD.deleted_at IS NOT NULL) THEN
        IF NOT kinship_exists(NEW.relation) THEN
            RAISE EXCEPTION 'cannot save: kinship is unknown or deleted';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER residencies_check_kinship BEFORE INSERT OR UPDATE ON residencies
    FOR EACH ROW EXECUTE FUNCTION residencies_check_kinship();

-- Запись справочника нельзя удалить, пока ею пользуются действующие проживающие.
CREATE FUNCTION kinships_check_children() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NOT NULL AND OLD.deleted_at IS NULL THEN
        IF EXISTS (SELECT 1 FROM residencies WHERE relation = OLD.code AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'cannot delete: has active residencies';
        END IF;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER kinships_check_children BEFORE UPDATE ON kinships
    FOR EACH ROW EXECUTE FUNCTION kinships_check_children();

-- Счётчики воды и электроэнергии и их показания.
-- Индивидуальный счётчик привязан к помещению, общедомовой — к дому (ровно одно из двух).
-- Электросчётчик бывает одно-, двух- и трёхтарифным: показания хранятся одной записью на дату (t1, t2, t3).
CREATE TABLE meters (
    id                    BIGSERIAL PRIMARY KEY,
    building_id           BIGINT REFERENCES buildings (id),
    premises_id           BIGINT REFERENCES premises (id),
    resource              TEXT NOT NULL CHECK (resource IN ('cold_water', 'hot_water', 'electricity')),
    tariffs               INT NOT NULL DEFAULT 1 CHECK (tariffs BETWEEN 1 AND 3),
    serial_number         TEXT NOT NULL,
    model                 TEXT,
    comment               TEXT,
    coefficient           NUMERIC(10, 3) NOT NULL DEFAULT 1 CHECK (coefficient > 0), -- коэффициент трансформации
    installed_at          DATE NOT NULL,                                              -- ввод в эксплуатацию
    removed_at            DATE,                                                       -- окончание: после этой даты счётчик не активен
    verified_at           DATE,                                                       -- дата последней поверки
    next_verification_at  DATE,
    initial_t1            NUMERIC(14, 3) NOT NULL DEFAULT 0 CHECK (initial_t1 >= 0), -- показания при установке
    initial_t2            NUMERIC(14, 3) NOT NULL DEFAULT 0 CHECK (initial_t2 >= 0),
    initial_t3            NUMERIC(14, 3) NOT NULL DEFAULT 0 CHECK (initial_t3 >= 0),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at            TIMESTAMPTZ,
    CONSTRAINT meters_scope_check CHECK ((building_id IS NULL) <> (premises_id IS NULL)),
    CONSTRAINT meters_water_tariffs_check CHECK (resource = 'electricity' OR tariffs = 1),
    CONSTRAINT meters_removed_check CHECK (removed_at IS NULL OR removed_at >= installed_at),
    CONSTRAINT meters_next_verification_check CHECK (next_verification_at IS NULL OR verified_at IS NULL OR next_verification_at >= verified_at)
);
CREATE INDEX meters_building_id_idx ON meters (building_id);
CREATE INDEX meters_premises_id_idx ON meters (premises_id);
CREATE UNIQUE INDEX meters_serial_number_key ON meters (resource, serial_number) WHERE deleted_at IS NULL;

CREATE TABLE meter_readings (
    id            BIGSERIAL PRIMARY KEY,
    meter_id      BIGINT NOT NULL REFERENCES meters (id),
    reading_date  DATE NOT NULL,
    t1            NUMERIC(14, 3) NOT NULL CHECK (t1 >= 0),
    t2            NUMERIC(14, 3) CHECK (t2 >= 0),
    t3            NUMERIC(14, 3) CHECK (t3 >= 0),
    source        TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'import')),
    comment       TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);
CREATE INDEX meter_readings_meter_id_idx ON meter_readings (meter_id, reading_date);
CREATE UNIQUE INDEX meter_readings_meter_date_key ON meter_readings (meter_id, reading_date) WHERE deleted_at IS NULL;

-- Целостность живых и удалённых записей (см. 0003_soft_delete).
CREATE FUNCTION meters_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('buildings', NEW.building_id, 'building');
        PERFORM assert_active('premises', NEW.premises_id, 'premises');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION meter_readings_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('meters', NEW.meter_id, 'meter');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE FUNCTION meters_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('meter_readings', 'meter_id', OLD.id, 'readings');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE TRIGGER meters_check_parents BEFORE INSERT OR UPDATE ON meters
    FOR EACH ROW EXECUTE FUNCTION meters_check_parents();
CREATE TRIGGER meter_readings_check_parents BEFORE INSERT OR UPDATE ON meter_readings
    FOR EACH ROW EXECUTE FUNCTION meter_readings_check_parents();
CREATE TRIGGER meters_check_children BEFORE UPDATE ON meters
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION meters_check_children();

-- Дом и помещение нельзя удалить, пока у них есть счётчики.
CREATE OR REPLACE FUNCTION buildings_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('premises', 'building_id', OLD.id, 'premises');
    PERFORM assert_no_active('meters', 'building_id', OLD.id, 'meters');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION premises_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('ownerships', 'premises_id', OLD.id, 'ownerships');
    PERFORM assert_no_active('residencies', 'premises_id', OLD.id, 'residencies');
    PERFORM assert_no_active('personal_accounts', 'premises_id', OLD.id, 'accounts');
    PERFORM assert_no_active('meters', 'premises_id', OLD.id, 'meters');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

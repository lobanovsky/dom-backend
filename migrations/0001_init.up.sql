CREATE TABLE organizations (
    id          BIGSERIAL PRIMARY KEY,
    kind        TEXT NOT NULL CHECK (kind IN ('uk', 'tsn', 'tszh')),
    name        TEXT NOT NULL,
    inn         TEXT,
    kpp         TEXT,
    ogrn        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE buildings (
    id                BIGSERIAL PRIMARY KEY,
    organization_id   BIGINT NOT NULL REFERENCES organizations (id),
    kind              TEXT NOT NULL CHECK (kind IN ('apartment_building', 'parking', 'common_premises', 'other')),
    address           TEXT NOT NULL,
    cadastral_number  TEXT,
    floors            INT CHECK (floors > 0),
    entrances         INT CHECK (entrances > 0),
    total_area        NUMERIC(12, 2) CHECK (total_area > 0),
    year_built        INT CHECK (year_built BETWEEN 1700 AND 2200),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX buildings_organization_id_idx ON buildings (organization_id);
CREATE UNIQUE INDEX buildings_cadastral_number_uq ON buildings (cadastral_number) WHERE cadastral_number IS NOT NULL;

CREATE TABLE premises (
    id                BIGSERIAL PRIMARY KEY,
    building_id       BIGINT NOT NULL REFERENCES buildings (id),
    kind              TEXT NOT NULL CHECK (kind IN ('apartment', 'non_residential', 'commercial', 'parking_space', 'storage')),
    number            TEXT NOT NULL,
    entrance          INT,
    floor             INT,
    total_area        NUMERIC(10, 2) CHECK (total_area > 0),
    living_area       NUMERIC(10, 2) CHECK (living_area > 0),
    rooms             INT CHECK (rooms > 0),
    cadastral_number  TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (building_id, kind, number)
);
CREATE UNIQUE INDEX premises_cadastral_number_uq ON premises (cadastral_number) WHERE cadastral_number IS NOT NULL;

CREATE TABLE persons (
    id          BIGSERIAL PRIMARY KEY,
    last_name   TEXT NOT NULL,
    first_name  TEXT NOT NULL,
    middle_name TEXT,
    birth_date  DATE,
    phone       TEXT,
    email       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE legal_entities (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    inn         TEXT NOT NULL,
    kpp         TEXT,
    ogrn        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX legal_entities_inn_kpp_uq ON legal_entities (inn, COALESCE(kpp, ''));

CREATE TABLE ownerships (
    id               BIGSERIAL PRIMARY KEY,
    premises_id      BIGINT NOT NULL REFERENCES premises (id),
    person_id        BIGINT REFERENCES persons (id),
    legal_entity_id  BIGINT REFERENCES legal_entities (id),
    share_num        INT NOT NULL DEFAULT 1 CHECK (share_num > 0),
    share_den        INT NOT NULL DEFAULT 1 CHECK (share_den > 0),
    valid_from       DATE NOT NULL,
    valid_to         DATE,
    basis            TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((person_id IS NULL) <> (legal_entity_id IS NULL)),
    CHECK (share_num <= share_den),
    CHECK (valid_to IS NULL OR valid_to >= valid_from)
);
CREATE INDEX ownerships_premises_id_idx ON ownerships (premises_id);
CREATE INDEX ownerships_person_id_idx ON ownerships (person_id);
CREATE INDEX ownerships_legal_entity_id_idx ON ownerships (legal_entity_id);

CREATE TABLE residencies (
    id                BIGSERIAL PRIMARY KEY,
    person_id         BIGINT NOT NULL REFERENCES persons (id),
    premises_id       BIGINT NOT NULL REFERENCES premises (id),
    registered        BOOLEAN NOT NULL DEFAULT false,
    relation          TEXT NOT NULL DEFAULT 'other' CHECK (relation IN ('spouse', 'child', 'parent', 'relative', 'tenant', 'other')),
    related_owner_id  BIGINT REFERENCES persons (id),
    valid_from        DATE NOT NULL,
    valid_to          DATE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (valid_to IS NULL OR valid_to >= valid_from)
);
CREATE INDEX residencies_person_id_idx ON residencies (person_id);
CREATE INDEX residencies_premises_id_idx ON residencies (premises_id);
CREATE INDEX residencies_related_owner_id_idx ON residencies (related_owner_id);

CREATE TABLE personal_accounts (
    id           BIGSERIAL PRIMARY KEY,
    number       TEXT NOT NULL UNIQUE,
    premises_id  BIGINT NOT NULL REFERENCES premises (id),
    purpose      TEXT NOT NULL DEFAULT 'utilities' CHECK (purpose IN ('utilities', 'capital_repair', 'parking', 'other')),
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'closed')),
    opened_at    DATE NOT NULL,
    closed_at    DATE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (closed_at IS NULL OR closed_at >= opened_at)
);
CREATE INDEX personal_accounts_premises_id_idx ON personal_accounts (premises_id);

CREATE TABLE account_holders (
    id               BIGSERIAL PRIMARY KEY,
    account_id       BIGINT NOT NULL REFERENCES personal_accounts (id),
    person_id        BIGINT REFERENCES persons (id),
    legal_entity_id  BIGINT REFERENCES legal_entities (id),
    valid_from       DATE NOT NULL,
    valid_to         DATE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((person_id IS NULL) <> (legal_entity_id IS NULL)),
    CHECK (valid_to IS NULL OR valid_to >= valid_from)
);
CREATE INDEX account_holders_account_id_idx ON account_holders (account_id);
CREATE INDEX account_holders_person_id_idx ON account_holders (person_id);
CREATE INDEX account_holders_legal_entity_id_idx ON account_holders (legal_entity_id);

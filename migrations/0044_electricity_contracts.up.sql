CREATE TABLE electricity_contracts (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE electricity_contract_meters (
    contract_id BIGINT NOT NULL REFERENCES electricity_contracts(id) ON DELETE CASCADE,
    meter_id BIGINT NOT NULL REFERENCES meters(id),
    PRIMARY KEY (contract_id, meter_id),
    UNIQUE (meter_id)
);

CREATE TABLE electricity_contract_prices (
    id BIGSERIAL PRIMARY KEY,
    contract_id BIGINT NOT NULL REFERENCES electricity_contracts(id) ON DELETE CASCADE,
    valid_from DATE NOT NULL,
    valid_to DATE,
    t1 NUMERIC(14,4) NOT NULL CHECK (t1 >= 0),
    t2 NUMERIC(14,4) CHECK (t2 IS NULL OR t2 >= 0),
    t3 NUMERIC(14,4) CHECK (t3 IS NULL OR t3 >= 0),
    CHECK (valid_to IS NULL OR valid_to >= valid_from),
    EXCLUDE USING gist (contract_id WITH =, daterange(valid_from, valid_to, '[]') WITH &&)
);

CREATE INDEX electricity_contract_meters_meter_idx ON electricity_contract_meters(meter_id);
CREATE INDEX electricity_contract_prices_period_idx ON electricity_contract_prices(contract_id, valid_from);

INSERT INTO electricity_contracts (id, name) VALUES
    (1, 'Дом'), (2, 'ИТП'), (3, 'Офисы'), (4, 'Паркинг');
SELECT setval(pg_get_serial_sequence('electricity_contracts', 'id'), 4, true);

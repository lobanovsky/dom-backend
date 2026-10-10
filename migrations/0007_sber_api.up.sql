-- Получение выписок из Sber API: текущие токены (одна строка, обновляются при каждом refresh) и журнал запусков.

CREATE TABLE sber_tokens (
    id               SMALLINT PRIMARY KEY CHECK (id = 1),
    access_token     TEXT NOT NULL,
    refresh_token    TEXT NOT NULL,
    access_expires_at TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sber_sync_runs (
    id            BIGSERIAL PRIMARY KEY,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ,
    trigger       TEXT NOT NULL CHECK (trigger IN ('schedule', 'manual')),
    date_from     DATE NOT NULL,
    date_to       DATE NOT NULL,
    accounts      INT NOT NULL DEFAULT 0,
    incoming      INT NOT NULL DEFAULT 0,
    outgoing      INT NOT NULL DEFAULT 0,
    skipped       INT NOT NULL DEFAULT 0,
    error         TEXT
);
CREATE INDEX sber_sync_runs_started_at_idx ON sber_sync_runs (started_at DESC);

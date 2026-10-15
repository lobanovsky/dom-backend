-- Забор реестров из почты (IMAP): журнал запусков и письма, уже разобранные сервером.
-- mail_messages нужна, чтобы не скачивать письма повторно; повторная загрузка файлов и так безопасна (дубли отсекает импорт).
-- Письма в почте не меняются: ящик читается только на чтение.
CREATE TABLE mail_sync_runs (
    id          BIGSERIAL PRIMARY KEY,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    trigger     TEXT NOT NULL CHECK (trigger IN ('schedule', 'manual')),
    messages    INT NOT NULL DEFAULT 0, -- новых писем разобрано
    files       INT NOT NULL DEFAULT 0, -- файлов-кандидатов в реестры
    imported    INT NOT NULL DEFAULT 0, -- реестров загружено
    payments    INT NOT NULL DEFAULT 0, -- платежей создано
    duplicates  INT NOT NULL DEFAULT 0, -- файлов, которые уже были загружены
    failed      INT NOT NULL DEFAULT 0, -- файлов с ошибкой или без известного счёта
    error       TEXT,
    details     JSONB NOT NULL DEFAULT '[]' -- результат по каждому файлу (не более 200)
);
CREATE INDEX mail_sync_runs_started_at_idx ON mail_sync_runs (started_at DESC);

CREATE TABLE mail_messages (
    id           BIGSERIAL PRIMARY KEY,
    folder       TEXT        NOT NULL,
    uid_validity BIGINT      NOT NULL, -- при смене UIDVALIDITY номера писем в папке начинаются заново
    uid          BIGINT      NOT NULL,
    message_id   TEXT        NOT NULL DEFAULT '',
    from_addr    TEXT        NOT NULL DEFAULT '',
    subject      TEXT        NOT NULL DEFAULT '',
    received_at  TIMESTAMPTZ,
    files        INT         NOT NULL DEFAULT 0,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (folder, uid_validity, uid)
);

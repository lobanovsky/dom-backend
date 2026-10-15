-- История входов: каждая попытка (успешная и нет) с адресом и браузером. Нужна, чтобы понимать, пользуются ли системой.
-- user_id пуст у попыток с неизвестным логином; username хранится как введён.
CREATE TABLE login_events (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      REFERENCES users (id),
    username   TEXT        NOT NULL,
    success    BOOLEAN     NOT NULL,
    ip         TEXT        NOT NULL DEFAULT '',
    user_agent TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX login_events_created_at_idx ON login_events (created_at DESC);
CREATE INDEX login_events_user_id_idx ON login_events (user_id, created_at DESC);

-- Последний успешный вход и последняя активность (обновляется не чаще раза в несколько минут).
ALTER TABLE users
    ADD COLUMN last_login_at TIMESTAMPTZ,
    ADD COLUMN last_seen_at  TIMESTAMPTZ;

-- Пользователи системы и их роли. Раньше была одна учётка из окружения (ADMIN_*):
-- теперь при пустой таблице администратор создаётся из неё при старте сервера (см. cmd/server).
--   admin  - полный доступ и управление пользователями;
--   viewer - только просмотр: чтение, скачивание Excel и файлов, но никаких изменений.
-- Пользователи не удаляются физически: их отключают (active = false).
-- token_version входит в токен сессии: смена пароля или отключение завершают прежние сессии.
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT        NOT NULL,
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL CHECK (role IN ('admin', 'viewer')),
    active        BOOLEAN     NOT NULL DEFAULT true,
    token_version INT         NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Логин не различает регистр. Имя индекса используется фронтендом для текста ошибки о дубликате.
CREATE UNIQUE INDEX users_username_key ON users (lower(username));

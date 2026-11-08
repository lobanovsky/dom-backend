-- Доступ к кабинету подтверждается сотрудником отдельно от импортированных контактов.
CREATE TABLE resident_access (
 person_id BIGINT PRIMARY KEY REFERENCES persons(id),
 email TEXT NOT NULL CHECK (email=lower(btrim(email))),
 enabled BOOLEAN NOT NULL DEFAULT true,
 approved_by BIGINT NOT NULL REFERENCES users(id),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX resident_access_email_key ON resident_access(email) WHERE enabled;
CREATE TABLE resident_accounts (
 id BIGSERIAL PRIMARY KEY,
 person_id BIGINT NOT NULL UNIQUE REFERENCES persons(id),
 email TEXT NOT NULL UNIQUE CHECK (email=lower(btrim(email))),
 password_hash TEXT NOT NULL,
 active BOOLEAN NOT NULL DEFAULT true,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE resident_tokens (
 token_hash TEXT PRIMARY KEY,
 person_id BIGINT NOT NULL REFERENCES persons(id),
 email TEXT NOT NULL,
 purpose TEXT NOT NULL CHECK(purpose IN ('activate','reset')),
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE resident_sessions (
 token_hash TEXT PRIMARY KEY,
 account_id BIGINT NOT NULL REFERENCES resident_accounts(id),
 expires_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE resident_reading_windows (
 building_id BIGINT PRIMARY KEY REFERENCES buildings(id),
 period DATE NOT NULL CHECK (extract(day FROM period)=1),
 opens_on DATE NOT NULL,
 closes_on DATE NOT NULL CHECK (closes_on>=opens_on),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE meter_readings DROP CONSTRAINT meter_readings_source_check;
ALTER TABLE meter_readings ADD CONSTRAINT meter_readings_source_check CHECK(source IN ('manual','import','resident'));
ALTER TABLE meter_readings ADD COLUMN resident_account_id BIGINT REFERENCES resident_accounts(id);
CREATE TABLE resident_reading_audit (
 id BIGSERIAL PRIMARY KEY,
 reading_id BIGINT NOT NULL REFERENCES meter_readings(id),
 account_id BIGINT NOT NULL REFERENCES resident_accounts(id),
 before_values JSONB,
 after_values JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX resident_reading_audit_reading_idx ON resident_reading_audit(reading_id,id);
CREATE INDEX resident_tokens_expiry_idx ON resident_tokens(expires_at);
CREATE INDEX resident_sessions_account_idx ON resident_sessions(account_id);
-- Письма отправляет фоновый обработчик; HTTP-ответ не зависит от SMTP.
CREATE TABLE resident_mail_outbox (
 id BIGSERIAL PRIMARY KEY,
 email TEXT NOT NULL,
 link TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 attempts INT NOT NULL DEFAULT 0,
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

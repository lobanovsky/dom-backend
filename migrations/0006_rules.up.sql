-- Правила определения лицевых счетов и история запусков (с откатом).
--
-- assigned_by у входящего платежа показывает, откуда привязка к лицевому счёту/категории:
--   registry - из номера лицевого счёта в реестре, manual - вручную, rule - по правилу.
-- Правила никогда не перезаписывают registry и manual.

ALTER TABLE incoming_payments
    ADD COLUMN assigned_by TEXT CHECK (assigned_by IN ('registry', 'manual', 'rule')),
    ADD COLUMN rule_id     BIGINT,
    ADD COLUMN run_id      BIGINT;

UPDATE incoming_payments
SET assigned_by = CASE WHEN registry_id IS NOT NULL THEN 'registry' ELSE 'manual' END
WHERE personal_account_id IS NOT NULL OR category_id IS NOT NULL;

CREATE TABLE payment_rules (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    position    INT NOT NULL DEFAULT 0,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    direction   TEXT NOT NULL DEFAULT 'incoming' CHECK (direction IN ('incoming')),
    match_mode  TEXT NOT NULL DEFAULT 'all' CHECK (match_mode IN ('all', 'any')),
    conditions  JSONB NOT NULL DEFAULT '[]',
    action      JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX payment_rules_position_idx ON payment_rules (position, id);

CREATE TABLE assignment_runs (
    id               BIGSERIAL PRIMARY KEY,
    mode             TEXT NOT NULL CHECK (mode IN ('unassigned', 'recompute')),
    filters          JSONB NOT NULL DEFAULT '{}',
    candidates       INT NOT NULL DEFAULT 0,
    assigned_count   INT NOT NULL DEFAULT 0,
    changed_count    INT NOT NULL DEFAULT 0,
    cleared_count    INT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    rolled_back_at   TIMESTAMPTZ,
    rolled_back_kept INT NOT NULL DEFAULT 0
);

-- Прежнее состояние каждого изменённого запуском платежа: по нему делается откат.
CREATE TABLE assignment_run_items (
    id                     BIGSERIAL PRIMARY KEY,
    run_id                 BIGINT NOT NULL REFERENCES assignment_runs (id) ON DELETE CASCADE,
    payment_id             BIGINT NOT NULL REFERENCES incoming_payments (id),
    prev_personal_account  BIGINT,
    prev_category          BIGINT,
    prev_assigned_by       TEXT,
    prev_rule_id           BIGINT,
    prev_run_id            BIGINT
);
CREATE INDEX assignment_run_items_run_id_idx ON assignment_run_items (run_id);
CREATE INDEX assignment_run_items_payment_id_idx ON assignment_run_items (payment_id);

ALTER TABLE incoming_payments
    ADD CONSTRAINT incoming_payments_rule_id_fkey FOREIGN KEY (rule_id) REFERENCES payment_rules (id),
    ADD CONSTRAINT incoming_payments_run_id_fkey FOREIGN KEY (run_id) REFERENCES assignment_runs (id);
CREATE INDEX incoming_payments_assigned_by_idx ON incoming_payments (assigned_by);

-- Правила по умолчанию. Остальные пользователь добавляет сам в интерфейсе.
INSERT INTO payment_rules (name, position, enabled, match_mode, conditions, action) VALUES
    ('Номер лицевого счёта в назначении платежа', 1, true, 'all', '[]',
     '{"type":"account_from_text","field":"purpose","pattern":"(?:^|\\D)(0000\\d{6})(?:\\D|$)"}'),
    ('Собственник по ФИО плательщика', 2, true, 'all', '[]',
     '{"type":"link_by_owner"}');

-- Правила и запуски определения категорий для исходящих платежей.
--
-- У исходящего платежа, как у входящего, assigned_by показывает происхождение категории:
-- manual - вручную, rule - по правилу. Правила не перезаписывают manual.

ALTER TABLE outgoing_payments
    ADD COLUMN assigned_by TEXT CHECK (assigned_by IN ('manual', 'rule')),
    ADD COLUMN rule_id     BIGINT,
    ADD COLUMN run_id      BIGINT;

UPDATE outgoing_payments SET assigned_by = 'manual' WHERE category_id IS NOT NULL;

ALTER TABLE outgoing_payments
    ADD CONSTRAINT outgoing_payments_rule_id_fkey FOREIGN KEY (rule_id) REFERENCES payment_rules (id),
    ADD CONSTRAINT outgoing_payments_run_id_fkey FOREIGN KEY (run_id) REFERENCES assignment_runs (id);
CREATE INDEX outgoing_payments_assigned_by_idx ON outgoing_payments (assigned_by);

ALTER TABLE payment_rules DROP CONSTRAINT payment_rules_direction_check;
ALTER TABLE payment_rules ADD CONSTRAINT payment_rules_direction_check CHECK (direction IN ('incoming', 'outgoing'));

ALTER TABLE assignment_runs
    ADD COLUMN direction TEXT NOT NULL DEFAULT 'incoming' CHECK (direction IN ('incoming', 'outgoing'));

-- payment_id указывает на входящий или исходящий платёж в зависимости от направления запуска.
ALTER TABLE assignment_run_items DROP CONSTRAINT assignment_run_items_payment_id_fkey;

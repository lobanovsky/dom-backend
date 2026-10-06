ALTER TABLE incoming_payments DROP CONSTRAINT incoming_payments_rule_id_fkey;
ALTER TABLE incoming_payments DROP CONSTRAINT incoming_payments_run_id_fkey;
DROP INDEX incoming_payments_assigned_by_idx;
DROP TABLE assignment_run_items;
DROP TABLE assignment_runs;
DROP TABLE payment_rules;
ALTER TABLE incoming_payments DROP COLUMN assigned_by, DROP COLUMN rule_id, DROP COLUMN run_id;

ALTER TABLE outgoing_payments DROP COLUMN assigned_by, DROP COLUMN rule_id, DROP COLUMN run_id;

DELETE FROM assignment_runs WHERE direction = 'outgoing'; -- записи о платежах уходят каскадом
DELETE FROM payment_rules WHERE direction = 'outgoing';

ALTER TABLE assignment_runs DROP COLUMN direction;
ALTER TABLE payment_rules DROP CONSTRAINT payment_rules_direction_check;
ALTER TABLE payment_rules ADD CONSTRAINT payment_rules_direction_check CHECK (direction IN ('incoming'));
ALTER TABLE assignment_run_items
    ADD CONSTRAINT assignment_run_items_payment_id_fkey FOREIGN KEY (payment_id) REFERENCES incoming_payments (id);

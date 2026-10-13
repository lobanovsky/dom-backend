DROP INDEX IF EXISTS incoming_payments_to_export_idx;
DROP INDEX IF EXISTS incoming_payments_export_id_idx;
ALTER TABLE incoming_payments DROP COLUMN export_id;
DROP TABLE payment_exports;

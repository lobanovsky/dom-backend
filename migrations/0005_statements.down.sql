CREATE OR REPLACE FUNCTION bank_accounts_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('payment_registries', 'bank_account_id', OLD.id, 'payment registries');
    PERFORM assert_no_active('incoming_payments', 'bank_account_id', OLD.id, 'incoming payments');
    PERFORM assert_no_active('outgoing_payments', 'bank_account_id', OLD.id, 'outgoing payments');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION outgoing_payments_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('bank_accounts', NEW.bank_account_id, 'bank account');
        PERFORM assert_active('payment_categories', NEW.category_id, 'payment category');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION incoming_payments_check_parents() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL THEN
        PERFORM assert_active('bank_accounts', NEW.bank_account_id, 'bank account');
        PERFORM assert_active('payment_registries', NEW.registry_id, 'payment registry');
        PERFORM assert_active('personal_accounts', NEW.personal_account_id, 'account');
        PERFORM assert_active('payment_categories', NEW.category_id, 'payment category');
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

DROP TRIGGER bank_statements_check_children ON bank_statements;
DROP TRIGGER bank_statements_check_parents ON bank_statements;
DROP FUNCTION bank_statements_check_children();
DROP FUNCTION bank_statements_check_parents();

DROP INDEX incoming_payments_dedup_key_key;
DROP INDEX outgoing_payments_dedup_key_key;
ALTER TABLE incoming_payments DROP COLUMN statement_id, DROP COLUMN dedup_key;
ALTER TABLE outgoing_payments DROP COLUMN statement_id, DROP COLUMN dedup_key;
DROP TABLE bank_statements;

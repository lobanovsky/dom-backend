CREATE OR REPLACE FUNCTION services_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('service_tariffs', 'service_id', OLD.id, 'tariffs');
    IF EXISTS (SELECT 1 FROM service_components c JOIN services p ON p.id = c.service_id AND p.deleted_at IS NULL WHERE c.component_id = OLD.id) THEN
        RAISE EXCEPTION 'cannot delete: has active composite services';
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION personal_accounts_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('account_holders', 'account_id', OLD.id, 'account holders');
    PERFORM assert_no_active('incoming_payments', 'personal_account_id', OLD.id, 'incoming payments');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

DROP TABLE account_services;
DROP FUNCTION account_services_check();
ALTER TABLE services DROP COLUMN receipt_name, DROP COLUMN premises_kinds, DROP COLUMN account_purpose;

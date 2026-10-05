CREATE OR REPLACE FUNCTION personal_accounts_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('account_holders', 'account_id', OLD.id, 'account holders');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION organizations_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('buildings', 'organization_id', OLD.id, 'buildings');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

DROP TABLE outgoing_payments;
DROP TABLE incoming_payments;
DROP TABLE payment_registries;
DROP TABLE payment_categories;
DROP TABLE bank_accounts;

DROP FUNCTION bank_accounts_check_parents();
DROP FUNCTION payment_registries_check_parents();
DROP FUNCTION incoming_payments_check_parents();
DROP FUNCTION outgoing_payments_check_parents();
DROP FUNCTION bank_accounts_check_children();
DROP FUNCTION payment_registries_check_children();
DROP FUNCTION payment_categories_check_children();

CREATE OR REPLACE FUNCTION organizations_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('buildings', 'organization_id', OLD.id, 'buildings');
    PERFORM assert_no_active('bank_accounts', 'organization_id', OLD.id, 'bank accounts');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

DROP FUNCTION service_price_on(BIGINT, DATE);
DROP TABLE service_tariffs;
DROP TABLE service_components;
DROP TABLE services;
DROP FUNCTION service_tariffs_check_parents();
DROP FUNCTION services_check_children();
DROP FUNCTION services_check_parents();

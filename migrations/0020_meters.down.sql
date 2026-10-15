CREATE OR REPLACE FUNCTION buildings_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('premises', 'building_id', OLD.id, 'premises');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION premises_check_children() RETURNS trigger AS $$
BEGIN
    PERFORM assert_no_active('ownerships', 'premises_id', OLD.id, 'ownerships');
    PERFORM assert_no_active('residencies', 'premises_id', OLD.id, 'residencies');
    PERFORM assert_no_active('personal_accounts', 'premises_id', OLD.id, 'accounts');
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

DROP TABLE meter_readings;
DROP TABLE meters;
DROP FUNCTION meter_readings_check_parents();
DROP FUNCTION meters_check_children();
DROP FUNCTION meters_check_parents();

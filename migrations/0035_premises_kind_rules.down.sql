DROP TRIGGER premises_kinds_check_rules ON premises_kinds;
DROP FUNCTION premises_kinds_check_rules();
DROP TRIGGER premises_check_kind_rules ON premises;
DROP FUNCTION premises_check_kind_rules();
DROP TRIGGER ownerships_check_single_owner ON ownerships;
DROP FUNCTION ownerships_check_single_owner();
DROP TRIGGER residencies_check_premises_kind ON residencies;
DROP FUNCTION residencies_check_premises_kind();
ALTER TABLE premises_kinds DROP COLUMN single_owner, DROP COLUMN no_residents;

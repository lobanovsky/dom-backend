-- Откат физически удаляет все помеченные удалёнными записи (в порядке от
-- зависимых к главным): иначе вернуть прежние уникальные ограничения нельзя.
DROP TRIGGER organizations_check_children ON organizations;
DROP TRIGGER buildings_check_children ON buildings;
DROP TRIGGER premises_check_children ON premises;
DROP TRIGGER persons_check_children ON persons;
DROP TRIGGER legal_entities_check_children ON legal_entities;
DROP TRIGGER personal_accounts_check_children ON personal_accounts;
DROP TRIGGER buildings_check_parents ON buildings;
DROP TRIGGER premises_check_parents ON premises;
DROP TRIGGER ownerships_check_parents ON ownerships;
DROP TRIGGER residencies_check_parents ON residencies;
DROP TRIGGER personal_accounts_check_parents ON personal_accounts;
DROP TRIGGER account_holders_check_parents ON account_holders;

DROP FUNCTION organizations_check_children();
DROP FUNCTION buildings_check_children();
DROP FUNCTION premises_check_children();
DROP FUNCTION persons_check_children();
DROP FUNCTION legal_entities_check_children();
DROP FUNCTION personal_accounts_check_children();
DROP FUNCTION buildings_check_parents();
DROP FUNCTION premises_check_parents();
DROP FUNCTION ownerships_check_parents();
DROP FUNCTION residencies_check_parents();
DROP FUNCTION personal_accounts_check_parents();
DROP FUNCTION account_holders_check_parents();
DROP FUNCTION assert_no_active(regclass, text, bigint, text);
DROP FUNCTION assert_active(regclass, bigint, text);

DELETE FROM account_holders WHERE deleted_at IS NOT NULL;
DELETE FROM ownerships WHERE deleted_at IS NOT NULL;
DELETE FROM residencies WHERE deleted_at IS NOT NULL;
DELETE FROM personal_accounts WHERE deleted_at IS NOT NULL;
DELETE FROM premises WHERE deleted_at IS NOT NULL;
DELETE FROM persons WHERE deleted_at IS NOT NULL;
DELETE FROM legal_entities WHERE deleted_at IS NOT NULL;
DELETE FROM buildings WHERE deleted_at IS NOT NULL;
DELETE FROM organizations WHERE deleted_at IS NOT NULL;

DROP INDEX legal_entities_inn_kpp_uq;
CREATE UNIQUE INDEX legal_entities_inn_kpp_uq ON legal_entities (inn, COALESCE(kpp, ''));

DROP INDEX buildings_cadastral_number_uq;
CREATE UNIQUE INDEX buildings_cadastral_number_uq ON buildings (cadastral_number) WHERE cadastral_number IS NOT NULL;

DROP INDEX premises_cadastral_number_uq;
CREATE UNIQUE INDEX premises_cadastral_number_uq ON premises (cadastral_number) WHERE cadastral_number IS NOT NULL;

DROP INDEX personal_accounts_number_key;
ALTER TABLE personal_accounts ADD CONSTRAINT personal_accounts_number_key UNIQUE (number);

DROP INDEX premises_building_id_kind_number_key;
ALTER TABLE premises ADD CONSTRAINT premises_building_id_kind_number_key UNIQUE (building_id, kind, number);

ALTER TABLE account_holders   DROP COLUMN deleted_at;
ALTER TABLE personal_accounts DROP COLUMN deleted_at;
ALTER TABLE residencies       DROP COLUMN deleted_at;
ALTER TABLE ownerships        DROP COLUMN deleted_at;
ALTER TABLE legal_entities    DROP COLUMN deleted_at;
ALTER TABLE persons           DROP COLUMN deleted_at;
ALTER TABLE premises          DROP COLUMN deleted_at;
ALTER TABLE buildings         DROP COLUMN deleted_at;
ALTER TABLE organizations     DROP COLUMN deleted_at;

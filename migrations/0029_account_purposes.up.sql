-- Лицевые счета бывают только двух типов: ЖКУ и капремонт. Типы «паркинг» и «другое» не использовались.
-- Если в базе остались счета или услуги таких типов (в том числе удалённые), миграция останавливается, ничего не меняя.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM personal_accounts WHERE purpose NOT IN ('utilities', 'capital_repair')) THEN
        RAISE EXCEPTION 'personal_accounts has purpose other than utilities and capital_repair: fix the data first';
    END IF;
    IF EXISTS (SELECT 1 FROM services WHERE account_purpose NOT IN ('utilities', 'capital_repair')) THEN
        RAISE EXCEPTION 'services has account_purpose other than utilities and capital_repair: fix the data first';
    END IF;
END $$;

ALTER TABLE personal_accounts DROP CONSTRAINT personal_accounts_purpose_check;
ALTER TABLE personal_accounts ADD CONSTRAINT personal_accounts_purpose_check CHECK (purpose IN ('utilities', 'capital_repair'));

ALTER TABLE services DROP CONSTRAINT services_account_purpose_check;
ALTER TABLE services ADD CONSTRAINT services_account_purpose_check CHECK (account_purpose IN ('utilities', 'capital_repair'));

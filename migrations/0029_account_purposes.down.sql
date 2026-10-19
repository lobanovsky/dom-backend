ALTER TABLE services DROP CONSTRAINT services_account_purpose_check;
ALTER TABLE services ADD CONSTRAINT services_account_purpose_check CHECK (account_purpose IN ('utilities', 'capital_repair', 'parking', 'other'));

ALTER TABLE personal_accounts DROP CONSTRAINT personal_accounts_purpose_check;
ALTER TABLE personal_accounts ADD CONSTRAINT personal_accounts_purpose_check CHECK (purpose IN ('utilities', 'capital_repair', 'parking', 'other'));

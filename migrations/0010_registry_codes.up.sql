-- Несколько кодов реестра на счёт: у ВТБ имена файлов менялись (311023_VTB_224416, 081026_VTB_231068_001),
-- и все они относятся к одному счёту. Заменяет registry_code из 0009.
ALTER TABLE bank_accounts ADD COLUMN registry_codes TEXT[] NOT NULL DEFAULT '{}';
UPDATE bank_accounts SET registry_codes = ARRAY[registry_code] WHERE registry_code IS NOT NULL;
DROP INDEX bank_accounts_registry_code_key;
ALTER TABLE bank_accounts DROP COLUMN registry_code;

-- Код принадлежит одному неудалённому счёту (без учёта регистра). Индексом массив не проверить, поэтому триггер;
-- ошибка unique_violation с именем ограничения превращается в «already exists: ...» (409), как у обычных индексов.
CREATE FUNCTION bank_accounts_registry_codes_unique() RETURNS trigger AS $$
BEGIN
    IF NEW.deleted_at IS NULL AND cardinality(NEW.registry_codes) > 0 AND EXISTS (
        SELECT 1 FROM bank_accounts b, unnest(b.registry_codes) c
        WHERE b.id <> NEW.id AND b.deleted_at IS NULL
          AND lower(c) IN (SELECT lower(n) FROM unnest(NEW.registry_codes) n)
    ) THEN
        RAISE EXCEPTION 'registry code is already used by another bank account'
            USING ERRCODE = 'unique_violation', CONSTRAINT = 'bank_accounts_registry_codes_key';
    END IF;
    RETURN NEW;
END $$ LANGUAGE plpgsql;

CREATE TRIGGER bank_accounts_registry_codes_unique BEFORE INSERT OR UPDATE ON bank_accounts
    FOR EACH ROW EXECUTE FUNCTION bank_accounts_registry_codes_unique();

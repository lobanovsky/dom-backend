DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM residencies WHERE relation NOT IN ('spouse', 'child', 'parent', 'relative', 'tenant', 'other')) THEN
        RAISE EXCEPTION 'residencies has a custom kinship: fix the data first';
    END IF;
END $$;

DROP TRIGGER kinships_check_children ON kinships;
DROP FUNCTION kinships_check_children();
DROP TRIGGER residencies_check_kinship ON residencies;
DROP FUNCTION residencies_check_kinship();
DROP FUNCTION kinship_exists(text);

ALTER TABLE residencies ADD CONSTRAINT residencies_relation_check CHECK (relation IN ('spouse', 'child', 'parent', 'relative', 'tenant', 'other'));
DROP TABLE kinships;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM premises WHERE kind NOT IN ('apartment', 'non_residential', 'commercial', 'parking_space', 'storage')) THEN
        RAISE EXCEPTION 'premises has a custom kind: fix the data first';
    END IF;
    IF EXISTS (SELECT 1 FROM services WHERE NOT premises_kinds <@ ARRAY['apartment', 'non_residential', 'commercial', 'parking_space', 'storage']::text[]) THEN
        RAISE EXCEPTION 'services has a custom premises kind: fix the data first';
    END IF;
END $$;

DROP TRIGGER premises_kinds_check_children ON premises_kinds;
DROP FUNCTION premises_kinds_check_children();
DROP TRIGGER services_check_kinds ON services;
DROP FUNCTION services_check_kinds();
DROP TRIGGER premises_check_kind ON premises;
DROP FUNCTION premises_check_kind();
DROP FUNCTION premises_kind_exists(text);

ALTER TABLE premises ADD CONSTRAINT premises_kind_check CHECK (kind IN ('apartment', 'non_residential', 'commercial', 'parking_space', 'storage'));
ALTER TABLE services ADD CONSTRAINT services_premises_kinds_check CHECK (premises_kinds <@ ARRAY['apartment', 'non_residential', 'commercial', 'parking_space', 'storage']::text[]);
DROP TABLE premises_kinds;

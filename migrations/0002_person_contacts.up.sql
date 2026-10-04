-- У физлица может быть несколько телефонов и email. Порядок в массиве значим:
-- первый элемент — основной контакт.
ALTER TABLE persons
    ADD COLUMN phones TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN emails TEXT[] NOT NULL DEFAULT '{}';

UPDATE persons SET phones = ARRAY[btrim(phone)] WHERE phone IS NOT NULL AND btrim(phone) <> '';
UPDATE persons SET emails = ARRAY[btrim(email)] WHERE email IS NOT NULL AND btrim(email) <> '';

ALTER TABLE persons
    DROP COLUMN phone,
    DROP COLUMN email;

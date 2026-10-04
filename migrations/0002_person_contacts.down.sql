-- Откат оставляет только основной (первый) контакт; остальные теряются.
ALTER TABLE persons
    ADD COLUMN phone TEXT,
    ADD COLUMN email TEXT;

UPDATE persons SET phone = phones[1], email = emails[1];

ALTER TABLE persons
    DROP COLUMN phones,
    DROP COLUMN emails;

ALTER TABLE payment_categories
    ADD COLUMN color TEXT CHECK (color ~ '^#[0-9a-fA-F]{6}$'),
    ADD COLUMN icon  TEXT CHECK (char_length(icon) BETWEEN 1 AND 16);

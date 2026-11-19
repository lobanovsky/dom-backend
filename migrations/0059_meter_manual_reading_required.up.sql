ALTER TABLE meters
    ADD COLUMN manual_reading_required BOOLEAN NOT NULL DEFAULT false;

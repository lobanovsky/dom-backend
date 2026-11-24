ALTER TABLE meters
    ADD COLUMN default_t1 NUMERIC(14,3),
    ADD COLUMN default_t2 NUMERIC(14,3),
    ADD COLUMN default_t3 NUMERIC(14,3),
    ADD CONSTRAINT meters_default_readings_nonnegative CHECK (
        (default_t1 IS NULL OR default_t1 >= 0) AND
        (default_t2 IS NULL OR default_t2 >= 0) AND
        (default_t3 IS NULL OR default_t3 >= 0)
    );

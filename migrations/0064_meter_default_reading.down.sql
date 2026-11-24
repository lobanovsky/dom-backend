ALTER TABLE meters
    DROP CONSTRAINT meters_default_readings_nonnegative,
    DROP COLUMN default_t1,
    DROP COLUMN default_t2,
    DROP COLUMN default_t3;

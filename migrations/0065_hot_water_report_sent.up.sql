CREATE TABLE hot_water_report_sends (
    period DATE PRIMARY KEY CHECK (EXTRACT(DAY FROM period) = 1),
    sent_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

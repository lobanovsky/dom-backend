CREATE TABLE debtor_reports (
    period DATE PRIMARY KEY CHECK (EXTRACT(DAY FROM period) = 1),
    file_name TEXT NOT NULL,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE debtor_report_rows (
    id BIGSERIAL PRIMARY KEY,
    period DATE NOT NULL REFERENCES debtor_reports(period) ON DELETE CASCADE,
    account_number TEXT NOT NULL,
    debt NUMERIC(14,2) NOT NULL,
    UNIQUE (period, account_number)
);

CREATE INDEX debtor_report_rows_period_debt_idx ON debtor_report_rows (period, debt DESC);

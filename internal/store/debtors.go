package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type Debtors struct{ pool *pgxpool.Pool }

func NewDebtors(pool *pgxpool.Pool) *Debtors { return &Debtors{pool: pool} }

func (s *Debtors) Import(ctx context.Context, period model.Date, fileName string, items []model.DebtorImportRow) (model.DebtorImportResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.DebtorImportResult{}, err
	}
	defer tx.Rollback(ctx)
	accountNumbers := make([]string, 0, len(items))
	for _, item := range items {
		accountNumbers = append(accountNumbers, item.AccountNumber)
	}
	knownRows, err := tx.Query(ctx, `SELECT number FROM personal_accounts WHERE deleted_at IS NULL AND number = ANY($1::text[])`, accountNumbers)
	if err != nil {
		return model.DebtorImportResult{}, err
	}
	known := make(map[string]bool, len(items))
	for knownRows.Next() {
		var number string
		if err = knownRows.Scan(&number); err != nil {
			knownRows.Close()
			return model.DebtorImportResult{}, err
		}
		known[number] = true
	}
	if err = knownRows.Err(); err != nil {
		knownRows.Close()
		return model.DebtorImportResult{}, err
	}
	knownRows.Close()
	if _, err = tx.Exec(ctx, `INSERT INTO debtor_reports (period,file_name) VALUES ($1,$2)
		ON CONFLICT (period) DO UPDATE SET file_name=EXCLUDED.file_name, imported_at=now()`, period, fileName); err != nil {
		return model.DebtorImportResult{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM debtor_report_rows WHERE period=$1`, period); err != nil {
		return model.DebtorImportResult{}, err
	}
	rowsCount := 0
	for _, item := range items {
		if !known[item.AccountNumber] {
			continue
		}
		if _, err = tx.Exec(ctx, `INSERT INTO debtor_report_rows (period,account_number,debt) VALUES ($1,$2,$3)`, period, item.AccountNumber, item.Debt); err != nil {
			return model.DebtorImportResult{}, err
		}
		rowsCount++
	}
	if err = tx.Commit(ctx); err != nil {
		return model.DebtorImportResult{}, err
	}
	return model.DebtorImportResult{Period: period, RowsCount: rowsCount}, nil
}

func (s *Debtors) ListReports(ctx context.Context) ([]model.DebtorReport, error) {
	return collect[model.DebtorReport](s.pool.Query(ctx, `SELECT r.period,r.file_name,r.imported_at,
		(SELECT count(*)::int FROM debtor_report_rows x
		 JOIN personal_accounts a ON a.number=x.account_number AND a.deleted_at IS NULL
		 JOIN premises p ON p.id=a.premises_id AND p.deleted_at IS NULL
		 WHERE x.period=r.period) AS rows_count
		FROM debtor_reports r ORDER BY r.period DESC`))
}

func (s *Debtors) DeleteReport(ctx context.Context, period model.Date) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM debtor_reports WHERE period=$1`, period)
	return checkDeleted(tag, err)
}

func (s *Debtors) List(ctx context.Context, period model.Date) ([]model.DebtorReportRow, error) {
	return collect[model.DebtorReportRow](s.pool.Query(ctx, `SELECT x.account_number,x.debt::float8 AS debt,
		a.id AS account_id,a.purpose,p.id AS premises_id,p.kind AS premises_kind,p.number AS premises_number,
		COALESCE(h.names,ARRAY[]::text[]) AS holder_names,
		COALESCE(h.phones,ARRAY[]::text[]) AS phones,COALESCE(h.emails,ARRAY[]::text[]) AS emails
		FROM debtor_report_rows x
		JOIN personal_accounts a ON a.number=x.account_number AND a.deleted_at IS NULL
		JOIN premises p ON p.id=a.premises_id AND p.deleted_at IS NULL
		LEFT JOIN LATERAL (
			SELECT array_agg(DISTINCT COALESCE(NULLIF(concat_ws(' ',person.last_name,person.first_name,person.middle_name),''),entity.name)) FILTER (WHERE COALESCE(person.id,entity.id) IS NOT NULL) AS names,
				array_agg(DISTINCT phone.value) FILTER (WHERE phone.value IS NOT NULL) AS phones,
				array_agg(DISTINCT email.value) FILTER (WHERE email.value IS NOT NULL) AS emails
			FROM ownerships owner
			LEFT JOIN persons person ON person.id=owner.person_id AND person.deleted_at IS NULL
			LEFT JOIN legal_entities entity ON entity.id=owner.legal_entity_id AND entity.deleted_at IS NULL
			LEFT JOIN LATERAL unnest(person.phones) phone(value) ON true
			LEFT JOIN LATERAL unnest(person.emails) email(value) ON true
			WHERE owner.premises_id=p.id AND owner.deleted_at IS NULL
			AND owner.valid_from <= (($1::date + interval '1 month - 1 day')::date)
			AND (owner.valid_to IS NULL OR owner.valid_to >= (($1::date + interval '1 month - 1 day')::date))
		) h ON true
		WHERE x.period=$1 ORDER BY x.debt DESC,x.account_number`, period))
}

package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

const accountServiceSelect = `SELECT x.id, x.account_id, x.service_id, x.valid_from, x.valid_to, x.quantity, x.comment,
	a.number AS account_number, p.id AS premises_id, p.kind AS premises_kind, p.number AS premises_number, b.address AS building_address,
	s.name AS service_name, s.unit AS service_unit, service_price_on(s.id, CURRENT_DATE) AS current_price,
	x.created_at, x.updated_at, x.deleted_at
	FROM account_services x
	JOIN personal_accounts a ON a.id = x.account_id
	JOIN premises p ON p.id = a.premises_id
	JOIN buildings b ON b.id = p.building_id
	JOIN services s ON s.id = x.service_id`

type AccountServices struct{ pool *pgxpool.Pool }

func NewAccountServices(pool *pgxpool.Pool) *AccountServices { return &AccountServices{pool} }

func (s *AccountServices) List(ctx context.Context, f model.AccountServiceFilter, limit, offset int) ([]model.AccountService, error) {
	return collect[model.AccountService](s.pool.Query(ctx,
		accountServiceSelect+`
		 WHERE ($1::bigint IS NULL OR x.account_id = $1)
		   AND ($2::bigint IS NULL OR x.service_id = $2)
		   AND ($3::bigint IS NULL OR p.id = $3)
		   AND ($4::bigint IS NULL OR b.id = $4)
		   AND ($5::date IS NULL OR (x.valid_from <= $5 AND (x.valid_to IS NULL OR x.valid_to >= $5)))
		   AND (x.deleted_at IS NOT NULL) = $6
		 ORDER BY substring(p.number from '\d+')::numeric NULLS LAST, p.number, s.name, x.valid_from DESC, x.id LIMIT $7 OFFSET $8`,
		f.AccountID, f.ServiceID, f.PremisesID, f.BuildingID, f.ActiveOn, f.Deleted, limit, offset))
}

func (s *AccountServices) Get(ctx context.Context, id int64) (model.AccountService, error) {
	return one[model.AccountService](s.pool.Query(ctx, accountServiceSelect+` WHERE x.id = $1`, id))
}

// Create записывает назначение; соответствие услуги счёту, количество и пересечение периодов проверяет БД.
func (s *AccountServices) Create(ctx context.Context, in model.AccountService) (model.AccountService, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO account_services (account_id, service_id, valid_from, valid_to, quantity, comment) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		in.AccountID, in.ServiceID, in.ValidFrom, in.ValidTo, in.Quantity, in.Comment).Scan(&id)
	if err != nil {
		return model.AccountService{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

func (s *AccountServices) Update(ctx context.Context, id int64, in model.AccountService) (model.AccountService, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE account_services SET account_id = $2, service_id = $3, valid_from = $4, valid_to = $5, quantity = $6, comment = $7, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id, in.AccountID, in.ServiceID, in.ValidFrom, in.ValidTo, in.Quantity, in.Comment)
	if err := checkDeleted(tag, err); err != nil {
		return model.AccountService{}, err
	}
	return s.Get(ctx, id)
}

// Restore возвращает удалённое назначение; если период теперь пересекается с другим, БД вернёт конфликт.
func (s *AccountServices) Restore(ctx context.Context, id int64) (model.AccountService, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE account_services SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	if err := checkDeleted(tag, err); err != nil {
		return model.AccountService{}, err
	}
	return s.Get(ctx, id)
}

func (s *AccountServices) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "account_services", id)
}

// bulkCandidates — счета, подходящие услуге: тип счёта, вид помещения, дом; has — услуга уже назначена на пересекающийся период.
const bulkCandidates = `
	SELECT a.id AS account_id, a.number AS account_number, p.kind AS premises_kind, p.number AS premises_number, b.address AS building_address,
	       EXISTS (SELECT 1 FROM account_services x
	               WHERE x.account_id = a.id AND x.service_id = $1 AND x.deleted_at IS NULL
	                 AND daterange(x.valid_from, x.valid_to, '[]') && daterange($2::date, $3::date, '[]')) AS has
	FROM personal_accounts a
	JOIN premises p ON p.id = a.premises_id AND p.deleted_at IS NULL
	JOIN buildings b ON b.id = p.building_id AND b.deleted_at IS NULL
	JOIN services s ON s.id = $1
	WHERE a.deleted_at IS NULL AND a.status = 'active' AND a.purpose = s.account_purpose
	  AND (cardinality(s.premises_kinds) = 0 OR p.kind = ANY (s.premises_kinds))
	  AND (cardinality($4::text[]) = 0 OR p.kind = ANY ($4))
	  AND ($5::bigint IS NULL OR b.id = $5)`

type bulkRow struct {
	AccountID       int64  `db:"account_id"`
	AccountNumber   string `db:"account_number"`
	PremisesKind    string `db:"premises_kind"`
	PremisesNumber  string `db:"premises_number"`
	BuildingAddress string `db:"building_address"`
	Has             bool   `db:"has"`
}

// checkBulkService проверяет услугу массового назначения: она существует, действует, количество согласовано со способом расчёта.
func checkBulkService(ctx context.Context, q pgx.Tx, req model.AccountServiceBulkRequest) error {
	var method string
	var active bool
	err := q.QueryRow(ctx, `SELECT calc_method, active FROM services WHERE id = $1 AND deleted_at IS NULL`, req.ServiceID).Scan(&method, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid("service_id: service not found")
	}
	if err != nil {
		return mapErr(err)
	}
	if !active {
		return invalid("service is not active")
	}
	if method == "manual" && req.Quantity == nil {
		return invalid("quantity: is required for this service")
	}
	if method != "manual" && req.Quantity != nil {
		return invalid("quantity: must be empty for this service")
	}
	return nil
}

func bulkArgs(req model.AccountServiceBulkRequest) []any {
	kinds := req.PremisesKinds
	if kinds == nil {
		kinds = []string{}
	}
	return []any{req.ServiceID, req.ValidFrom, req.ValidTo, kinds, req.BuildingID}
}

// BulkPreview показывает, каким счетам будет назначена услуга, ничего не записывая.
func (s *AccountServices) BulkPreview(ctx context.Context, req model.AccountServiceBulkRequest) (model.AccountServiceBulkPreview, error) {
	if err := req.Validate(); err != nil {
		return model.AccountServiceBulkPreview{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.AccountServiceBulkPreview{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	if err := checkBulkService(ctx, tx, req); err != nil {
		return model.AccountServiceBulkPreview{}, err
	}
	rows, err := collect[bulkRow](tx.Query(ctx, bulkCandidates+`
		ORDER BY b.address, substring(p.number from '\d+')::numeric NULLS LAST, p.number, a.id`, bulkArgs(req)...))
	if err != nil {
		return model.AccountServiceBulkPreview{}, err
	}
	out := model.AccountServiceBulkPreview{Candidates: len(rows), Sample: []model.AccountServiceBulkSample{}}
	for _, r := range rows {
		if r.Has {
			out.SkippedExisting++
			continue
		}
		out.ToAssign++
		if len(out.Sample) < 10 {
			out.Sample = append(out.Sample, model.AccountServiceBulkSample{AccountID: r.AccountID, AccountNumber: r.AccountNumber,
				PremisesKind: r.PremisesKind, PremisesNumber: r.PremisesNumber, BuildingAddress: r.BuildingAddress})
		}
	}
	return out, nil
}

// BulkAssign назначает услугу всем подходящим счетам, у которых её ещё нет, одной транзакцией. Повторный вызов ничего не создаёт.
func (s *AccountServices) BulkAssign(ctx context.Context, req model.AccountServiceBulkRequest) (model.AccountServiceBulkResult, error) {
	if err := req.Validate(); err != nil {
		return model.AccountServiceBulkResult{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.AccountServiceBulkResult{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	if err := checkBulkService(ctx, tx, req); err != nil {
		return model.AccountServiceBulkResult{}, err
	}
	args := append(bulkArgs(req), req.Quantity)
	tag, err := tx.Exec(ctx,
		`INSERT INTO account_services (account_id, service_id, valid_from, valid_to, quantity)
		 SELECT c.account_id, $1, $2::date, $3::date, $6::numeric FROM (`+bulkCandidates+`) c WHERE NOT c.has`, args...)
	if err != nil {
		return model.AccountServiceBulkResult{}, mapErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.AccountServiceBulkResult{}, mapErr(err)
	}
	return model.AccountServiceBulkResult{Created: int(tag.RowsAffected())}, nil
}

// BulkClose закрывает датой назначения услуги, действовавшие на эту дату и продолжающиеся после неё (прекращение услуги).
func (s *AccountServices) BulkClose(ctx context.Context, req model.AccountServiceCloseRequest) (model.AccountServiceCloseResult, error) {
	if err := req.Validate(); err != nil {
		return model.AccountServiceCloseResult{}, err
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE account_services x SET valid_to = $2, updated_at = now()
		 FROM personal_accounts a, premises p
		 WHERE a.id = x.account_id AND p.id = a.premises_id
		   AND x.service_id = $1 AND x.deleted_at IS NULL
		   AND x.valid_from <= $2 AND (x.valid_to IS NULL OR x.valid_to > $2)
		   AND ($3::bigint IS NULL OR p.building_id = $3)`, req.ServiceID, req.ValidTo, req.BuildingID)
	if err != nil {
		return model.AccountServiceCloseResult{}, mapErr(err)
	}
	return model.AccountServiceCloseResult{Closed: int(tag.RowsAffected())}, nil
}

// periodMatched — назначения услуги, выбранные для смены периода; changes — период отличается от нового,
// conflict — новый период пересекается с другим действующим назначением этой услуги у того же счёта.
const periodMatched = `
	SELECT x.id, a.number AS account_number, p.kind AS premises_kind, p.number AS premises_number, b.address AS building_address,
	       x.valid_from, x.valid_to,
	       (x.valid_from, x.valid_to) IS DISTINCT FROM ($5::date, $6::date) AS changes,
	       EXISTS (SELECT 1 FROM account_services o
	               WHERE o.account_id = x.account_id AND o.service_id = x.service_id AND o.deleted_at IS NULL AND o.id <> x.id
	                 AND daterange(o.valid_from, o.valid_to, '[]') && daterange($5::date, $6::date, '[]')) AS conflict
	FROM account_services x
	JOIN personal_accounts a ON a.id = x.account_id
	JOIN premises p ON p.id = a.premises_id
	JOIN buildings b ON b.id = p.building_id
	WHERE x.service_id = $1 AND x.deleted_at IS NULL
	  AND ($2::bigint IS NULL OR b.id = $2)
	  AND (cardinality($3::text[]) = 0 OR p.kind = ANY ($3))
	  AND ($4::date IS NULL OR x.valid_from = $4)`

type periodRow struct {
	ID              int64       `db:"id"`
	AccountNumber   string      `db:"account_number"`
	PremisesKind    string      `db:"premises_kind"`
	PremisesNumber  string      `db:"premises_number"`
	BuildingAddress string      `db:"building_address"`
	ValidFrom       model.Date  `db:"valid_from"`
	ValidTo         *model.Date `db:"valid_to"`
	Changes         bool        `db:"changes"`
	Conflict        bool        `db:"conflict"`
}

func periodArgs(req model.AccountServicePeriodRequest) []any {
	kinds := req.PremisesKinds
	if kinds == nil {
		kinds = []string{}
	}
	return []any{req.ServiceID, req.BuildingID, kinds, req.CurrentValidFrom, req.ValidFrom, req.ValidTo}
}

func (r periodRow) sample() model.AccountServicePeriodSample {
	return model.AccountServicePeriodSample{AccountNumber: r.AccountNumber, PremisesKind: r.PremisesKind, PremisesNumber: r.PremisesNumber,
		BuildingAddress: r.BuildingAddress, ValidFrom: r.ValidFrom, ValidTo: r.ValidTo}
}

func loadPeriodRows(ctx context.Context, q pgx.Tx, req model.AccountServicePeriodRequest) ([]periodRow, error) {
	return collect[periodRow](q.Query(ctx, `SELECT * FROM (`+periodMatched+`) m
		ORDER BY m.building_address, substring(m.premises_number from '\d+')::numeric NULLS LAST, m.premises_number, m.id`, periodArgs(req)...))
}

// PeriodPreview показывает, какие назначения получат новый период, ничего не записывая.
func (s *AccountServices) PeriodPreview(ctx context.Context, req model.AccountServicePeriodRequest) (model.AccountServicePeriodPreview, error) {
	if err := req.Validate(); err != nil {
		return model.AccountServicePeriodPreview{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.AccountServicePeriodPreview{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	rows, err := loadPeriodRows(ctx, tx, req)
	if err != nil {
		return model.AccountServicePeriodPreview{}, err
	}
	out := model.AccountServicePeriodPreview{Matched: len(rows), Sample: []model.AccountServicePeriodSample{}, ConflictSample: []model.AccountServicePeriodSample{}}
	for _, r := range rows {
		switch {
		case !r.Changes:
			out.Unchanged++
		case r.Conflict:
			out.Conflicts++
			if len(out.ConflictSample) < 10 {
				out.ConflictSample = append(out.ConflictSample, r.sample())
			}
		default:
			out.ToChange++
			if len(out.Sample) < 10 {
				out.Sample = append(out.Sample, r.sample())
			}
		}
	}
	return out, nil
}

// PeriodApply заменяет период выбранных назначений одной транзакцией. Если у какого-то счёта новый период пересёкся бы
// с другим назначением этой услуги, не меняется ничего (422): сначала нужно разобраться с такими назначениями.
func (s *AccountServices) PeriodApply(ctx context.Context, req model.AccountServicePeriodRequest) (model.AccountServicePeriodResult, error) {
	if err := req.Validate(); err != nil {
		return model.AccountServicePeriodResult{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.AccountServicePeriodResult{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	rows, err := loadPeriodRows(ctx, tx, req)
	if err != nil {
		return model.AccountServicePeriodResult{}, err
	}
	var ids []int64
	conflicts := 0
	for _, r := range rows {
		if !r.Changes {
			continue
		}
		if r.Conflict {
			conflicts++
		}
		ids = append(ids, r.ID)
	}
	if conflicts > 0 {
		return model.AccountServicePeriodResult{}, invalid(fmt.Sprintf("valid_from: the new period overlaps other assignments of the same service on %d accounts", conflicts))
	}
	tag, err := tx.Exec(ctx, `UPDATE account_services SET valid_from = $2, valid_to = $3, updated_at = now() WHERE id = ANY ($1)`,
		ids, req.ValidFrom, req.ValidTo)
	if err != nil {
		return model.AccountServicePeriodResult{}, mapErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.AccountServicePeriodResult{}, mapErr(err)
	}
	return model.AccountServicePeriodResult{Changed: int(tag.RowsAffected())}, nil
}

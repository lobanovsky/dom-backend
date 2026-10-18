package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

const serviceSelect = `SELECT s.id, s.organization_id, s.kind, s.name, s.unit, s.calc_method, s.meter_resource, s.meter_tariff, s.account_purpose, s.premises_kinds, s.receipt_name, s.tariff_id, tf.name AS tariff_name,
	ARRAY(SELECT c.component_id FROM service_components c WHERE c.service_id = s.id ORDER BY c.component_id) AS component_ids,
	s.position, s.active, s.comment, o.name AS organization_name, service_price_on(s.id, CURRENT_DATE) AS current_price,
	s.created_at, s.updated_at, s.deleted_at
	FROM services s JOIN organizations o ON o.id = s.organization_id LEFT JOIN tariffs tf ON tf.id = s.tariff_id`

type Services struct{ pool *pgxpool.Pool }

func NewServices(pool *pgxpool.Pool) *Services { return &Services{pool} }

func (s *Services) List(ctx context.Context, f model.ServiceFilter, limit, offset int) ([]model.Service, error) {
	return collect[model.Service](s.pool.Query(ctx,
		serviceSelect+`
		 WHERE ($1::bigint IS NULL OR s.organization_id = $1)
		   AND ($2::text IS NULL OR s.kind = $2)
		   AND ($3::text IS NULL OR s.calc_method = $3)
		   AND ($4::bool IS NULL OR s.active = $4)
		   AND ($5::text IS NULL OR s.name ILIKE $5)
		   AND ($6::text IS NULL OR s.account_purpose = $6)
		   AND ($7::bigint IS NULL OR s.tariff_id = $7)
		   AND (s.deleted_at IS NOT NULL) = $8
		 ORDER BY s.kind, s.position, s.name, s.id LIMIT $9 OFFSET $10`,
		f.OrganizationID, f.Kind, f.CalcMethod, f.Active, likePattern(f.Q), f.AccountPurpose, f.TariffID, f.Deleted, limit, offset))
}

func (s *Services) Get(ctx context.Context, id int64) (model.Service, error) {
	return one[model.Service](s.pool.Query(ctx, serviceSelect+` WHERE s.id = $1`, id))
}

func (s *Services) Create(ctx context.Context, in model.Service) (model.Service, error) {
	in.SetDefaults()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Service{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO services (organization_id, kind, name, unit, calc_method, meter_resource, meter_tariff, account_purpose, premises_kinds, receipt_name, tariff_id, position, active, comment)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`,
		in.OrganizationID, in.Kind, in.Name, in.Unit, in.CalcMethod, in.MeterResource, in.MeterTariff, in.AccountPurpose, in.PremisesKinds, in.ReceiptName, in.TariffID,
		in.Position, in.Active, in.Comment).Scan(&id)
	if err != nil {
		return model.Service{}, mapErr(err)
	}
	if err := saveComponents(ctx, tx, id, in.ComponentIDs); err != nil {
		return model.Service{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Service{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

func (s *Services) Update(ctx context.Context, id int64, in model.Service) (model.Service, error) {
	in.SetDefaults()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.Service{}, mapErr(err)
	}
	defer rollback(ctx, tx)
	tag, err := tx.Exec(ctx,
		`UPDATE services SET organization_id = $2, kind = $3, name = $4, unit = $5, calc_method = $6, meter_resource = $7, meter_tariff = $8,
		        account_purpose = $9, premises_kinds = $10, receipt_name = $11, tariff_id = $12, position = $13, active = $14, comment = $15, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id, in.OrganizationID, in.Kind, in.Name, in.Unit, in.CalcMethod, in.MeterResource, in.MeterTariff, in.AccountPurpose, in.PremisesKinds, in.ReceiptName, in.TariffID,
		in.Position, in.Active, in.Comment)
	if err := checkDeleted(tag, err); err != nil {
		return model.Service{}, err
	}
	if err := checkAssignments(ctx, tx, id); err != nil {
		return model.Service{}, err
	}
	if err := checkNorms(ctx, tx, id); err != nil {
		return model.Service{}, err
	}
	if err := saveComponents(ctx, tx, id, in.ComponentIDs); err != nil {
		return model.Service{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Service{}, mapErr(err)
	}
	return s.Get(ctx, id)
}

// Restore снимает отметку удаления; если имя уже занято, БД вернёт ошибку.
func (s *Services) Restore(ctx context.Context, id int64) (model.Service, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE services SET deleted_at = NULL, updated_at = now() WHERE id = $1 AND deleted_at IS NOT NULL`, id)
	if err := checkDeleted(tag, err); err != nil {
		return model.Service{}, err
	}
	return s.Get(ctx, id)
}

func (s *Services) Delete(ctx context.Context, id int64) error {
	return softDelete(ctx, s.pool, "services", id)
}

// checkAssignments не даёт изменить услугу так, чтобы её действующие назначения перестали подходить: другой тип счёта,
// другие виды помещений или переход между ручным и расчётным количеством.
func checkAssignments(ctx context.Context, tx pgx.Tx, id int64) error {
	var bad bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM account_services x
		   JOIN services s ON s.id = x.service_id
		   JOIN personal_accounts a ON a.id = x.account_id
		   JOIN premises p ON p.id = a.premises_id
		   WHERE x.service_id = $1 AND x.deleted_at IS NULL
		     AND (a.purpose <> s.account_purpose
		          OR (cardinality(s.premises_kinds) > 0 AND NOT (p.kind = ANY (s.premises_kinds)))
		          OR (s.calc_method = 'manual') <> (x.quantity IS NOT NULL)))`, id).Scan(&bad)
	if err != nil {
		return mapErr(err)
	}
	if bad {
		return invalid("account_purpose: cannot be changed while accounts have this service assigned incompatibly")
	}
	return nil
}

// saveComponents заменяет состав услуги и проверяет составные связи услуги в обе стороны (она — сумма и она — слагаемое):
// сумма складывает простые услуги (не составные) той же организации и с той же единицей; у суммы нет собственных тарифов.
func saveComponents(ctx context.Context, tx pgx.Tx, id int64, components []int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM service_components WHERE service_id = $1`, id); err != nil {
		return mapErr(err)
	}
	for _, c := range components {
		if _, err := tx.Exec(ctx, `INSERT INTO service_components (service_id, component_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, c); err != nil {
			return mapErr(err)
		}
	}
	var bad bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM service_components c
		   JOIN services p ON p.id = c.service_id AND p.deleted_at IS NULL
		   JOIN services k ON k.id = c.component_id
		   WHERE (c.service_id = $1 OR c.component_id = $1)
		     AND (p.calc_method NOT IN ('sum_prices', 'sum_volumes') OR k.calc_method IN ('sum_prices', 'sum_volumes') OR k.deleted_at IS NOT NULL
		          OR p.organization_id <> k.organization_id OR p.unit <> k.unit OR p.account_purpose <> k.account_purpose))`, id).Scan(&bad)
	if err != nil {
		return mapErr(err)
	}
	if bad {
		return invalid("component_ids: components must be plain services of the same organization, unit and account purpose")
	}
	err = tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM services s JOIN service_tariffs t ON t.service_id = s.id AND t.deleted_at IS NULL
		                WHERE s.id = $1 AND s.calc_method = 'sum_prices')`, id).Scan(&bad)
	if err != nil {
		return mapErr(err)
	}
	if bad {
		return invalid("calc_method: a sum-of-prices service cannot have tariffs")
	}
	return nil
}

// checkNorms не даёт сменить способ расчёта услуги, пока у неё есть действующие нормативы домов.
func checkNorms(ctx context.Context, tx pgx.Tx, id int64) error {
	var bad bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM services s JOIN service_norms n ON n.service_id = s.id AND n.deleted_at IS NULL
		                WHERE s.id = $1 AND s.calc_method <> 'per_area_norm')`, id).Scan(&bad)
	if err != nil {
		return mapErr(err)
	}
	if bad {
		return invalid("calc_method: a service with norms must stay an area-norm service")
	}
	return nil
}

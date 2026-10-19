package store

import (
	"context"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/calc"
	"dom-backend/internal/model"
)

// Calculator собирает из БД данные для расчёта стоимости услуг лицевого счёта за месяц (пакет calc) и считает его.
type Calculator struct{ pool *pgxpool.Pool }

func NewCalculator(pool *pgxpool.Pool) *Calculator { return &Calculator{pool} }

// meterCalcSelect — счётчик с показанием расчётного месяца ($1) и предыдущим показанием (период раньше). Если в месяце
// несколько показаний (старые данные до привязки к месяцам), берётся последнее по дате.
const meterCalcSelect = `SELECT m.id, m.serial_number, COALESCE(m.name, ''), m.resource, m.tariffs, m.coefficient::text, m.installed_at, m.removed_at,
	m.initial_t1::text, m.initial_t2::text, m.initial_t3::text,
	cur.id, cur.t1::text, cur.t2::text, cur.t3::text,
	prev.id, prev.period, prev.t1::text, prev.t2::text, prev.t3::text
	FROM meters m
	LEFT JOIN LATERAL (
		SELECT r.id, r.t1, r.t2, r.t3 FROM meter_readings r
		WHERE r.meter_id = m.id AND r.deleted_at IS NULL AND r.period = $1
		ORDER BY r.reading_date DESC, r.id DESC LIMIT 1
	) cur ON true
	LEFT JOIN LATERAL (
		SELECT r.id, r.period, r.t1, r.t2, r.t3 FROM meter_readings r
		WHERE r.meter_id = m.id AND r.deleted_at IS NULL AND r.period < $1
		ORDER BY r.period DESC, r.reading_date DESC, r.id DESC LIMIT 1
	) prev ON true
	WHERE m.deleted_at IS NULL AND `

func parseRat(s *string) *big.Rat {
	if s == nil {
		return nil
	}
	r, ok := new(big.Rat).SetString(*s)
	if !ok {
		return nil
	}
	return r
}

func (c *Calculator) meters(ctx context.Context, period time.Time, cond string, args ...any) ([]calc.Meter, error) {
	rows, err := c.pool.Query(ctx, meterCalcSelect+cond, append([]any{period}, args...)...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []calc.Meter
	for rows.Next() {
		var (
			m                   calc.Meter
			coef                string
			init1, init2, init3 string
			curID, prevID       *int64
			cur1, cur2, cur3    *string
			prevPeriod          *time.Time
			prev1, prev2, prev3 *string
			installed           time.Time
			removed             *time.Time
		)
		if err := rows.Scan(&m.ID, &m.Serial, &m.Name, &m.Resource, &m.Tariffs, &coef, &installed, &removed, &init1, &init2, &init3,
			&curID, &cur1, &cur2, &cur3, &prevID, &prevPeriod, &prev1, &prev2, &prev3); err != nil {
			return nil, mapErr(err)
		}
		m.Coefficient, m.InstalledAt, m.RemovedAt = parseRat(&coef), installed, removed
		m.Initial = [3]*big.Rat{parseRat(&init1), parseRat(&init2), parseRat(&init3)}
		if curID != nil {
			m.Current = &calc.Reading{Period: period, T: [3]*big.Rat{parseRat(cur1), parseRat(cur2), parseRat(cur3)}}
		}
		if prevID != nil {
			m.Previous = &calc.Reading{Period: *prevPeriod, T: [3]*big.Rat{parseRat(prev1), parseRat(prev2), parseRat(prev3)}}
		}
		out = append(out, m)
	}
	return out, mapErr(rows.Err())
}

// AccountCalculation считает стоимость услуг лицевого счёта за месяц period (любая дата месяца). Ничего не записывает.
func (c *Calculator) AccountCalculation(ctx context.Context, accountID int64, period model.Date) (calc.Result, error) {
	first := period.FirstOfMonth().Time
	last := first.AddDate(0, 1, -1)

	in := calc.Input{Period: first, AccountID: accountID, Services: map[int64]*calc.Service{}}
	var premisesID, buildingID int64
	var area *string
	err := c.pool.QueryRow(ctx,
		`SELECT a.number, p.id, p.building_id, p.total_area::text FROM personal_accounts a JOIN premises p ON p.id = a.premises_id
		 WHERE a.id = $1 AND a.deleted_at IS NULL`, accountID).Scan(&in.AccountNumber, &premisesID, &buildingID, &area)
	if err != nil {
		if err == pgx.ErrNoRows {
			return calc.Result{}, &Error{ErrNotFound, "account not found"}
		}
		return calc.Result{}, mapErr(err)
	}
	in.Area = parseRat(area)

	// Услуги, назначенные в любой день месяца (выключенные услуги не начисляются).
	rows, err := c.pool.Query(ctx,
		`SELECT x.service_id, x.quantity::text FROM account_services x JOIN services s ON s.id = x.service_id
		 WHERE x.account_id = $1 AND x.deleted_at IS NULL AND s.deleted_at IS NULL AND s.active
		   AND x.valid_from <= $3 AND (x.valid_to IS NULL OR x.valid_to >= $2)
		 ORDER BY x.valid_from, x.id`, accountID, first, last)
	if err != nil {
		return calc.Result{}, mapErr(err)
	}
	var ids []int64
	for rows.Next() {
		var a calc.Assignment
		var q *string
		if err := rows.Scan(&a.ServiceID, &q); err != nil {
			rows.Close()
			return calc.Result{}, mapErr(err)
		}
		a.Quantity = parseRat(q)
		in.Assignments = append(in.Assignments, a)
		ids = append(ids, a.ServiceID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return calc.Result{}, mapErr(err)
	}

	// Услуги и их состав (составные услуги считают слагаемые, даже если те не назначены счёту).
	if err := c.loadServices(ctx, in.Services, ids, first, last, buildingID); err != nil {
		return calc.Result{}, err
	}
	var more []int64
	for _, s := range in.Services {
		for _, cid := range s.ComponentIDs {
			if in.Services[cid] == nil {
				more = append(more, cid)
			}
		}
	}
	if len(more) > 0 {
		if err := c.loadServices(ctx, in.Services, more, first, last, buildingID); err != nil {
			return calc.Result{}, err
		}
	}

	if in.Meters, err = c.meters(ctx, first, `m.premises_id = $2`, premisesID); err != nil {
		return calc.Result{}, err
	}
	for _, s := range in.Services {
		if s.Method != "common_meter" {
			continue
		}
		if s.CommonMeters, err = c.meters(ctx, first,
			`m.building_id = $3 AND m.id IN (SELECT meter_id FROM service_meters WHERE service_id = $2)`, s.ID, buildingID); err != nil {
			return calc.Result{}, err
		}
		// Расход делится между помещениями дома с этой услугой в месяце; помещение счёта учитывается всегда.
		var base *string
		err = c.pool.QueryRow(ctx,
			`SELECT sum(p.total_area)::text, count(*) FILTER (WHERE p.total_area IS NULL)
			 FROM premises p WHERE p.deleted_at IS NULL AND p.building_id = $2 AND (p.id = $5 OR EXISTS (
			   SELECT 1 FROM account_services x JOIN personal_accounts a ON a.id = x.account_id AND a.deleted_at IS NULL
			   WHERE a.premises_id = p.id AND x.service_id = $1 AND x.deleted_at IS NULL AND x.valid_from <= $4 AND (x.valid_to IS NULL OR x.valid_to >= $3)))`,
			s.ID, buildingID, first, last, premisesID).Scan(&base, &s.ShareNoArea)
		if err != nil {
			return calc.Result{}, mapErr(err)
		}
		s.ShareBase = parseRat(base)
	}
	return calc.Compute(in), nil
}

func (c *Calculator) loadServices(ctx context.Context, into map[int64]*calc.Service, ids []int64, first, last time.Time, buildingID int64) error {
	rows, err := c.pool.Query(ctx,
		`SELECT s.id, s.name, COALESCE(s.receipt_name, ''), s.kind, s.unit, s.calc_method, s.position, COALESCE(s.meter_resource, ''), COALESCE(s.meter_tariff, 1),
		        CASE WHEN s.calc_method = 'sum_prices' THEN NULL ELSE service_unit_price_on(s.id, $2)::text END,
		        CASE WHEN s.calc_method = 'sum_prices' THEN NULL ELSE service_unit_price_on(s.id, $3)::text END,
		        (SELECT n.value::text FROM service_norms n WHERE n.service_id = s.id AND n.building_id = $4 AND n.deleted_at IS NULL
		           AND n.valid_from <= $2 AND (n.valid_to IS NULL OR n.valid_to >= $2)),
		        (SELECT n.value::text FROM service_norms n WHERE n.service_id = s.id AND n.building_id = $4 AND n.deleted_at IS NULL
		           AND n.valid_from <= $3 AND (n.valid_to IS NULL OR n.valid_to >= $3)),
		        ARRAY(SELECT k.component_id FROM service_components k WHERE k.service_id = s.id ORDER BY k.component_id)
		 FROM services s WHERE s.id = ANY($1) AND s.deleted_at IS NULL`, ids, first, last, buildingID)
	if err != nil {
		return mapErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s calc.Service
		var pf, pl, nf, nl *string
		if err := rows.Scan(&s.ID, &s.Name, &s.ReceiptName, &s.Kind, &s.Unit, &s.Method, &s.Position, &s.MeterResource, &s.MeterTariff,
			&pf, &pl, &nf, &nl, &s.ComponentIDs); err != nil {
			return mapErr(err)
		}
		s.PriceFirst, s.PriceLast, s.NormFirst, s.NormLast = parseRat(pf), parseRat(pl), parseRat(nf), parseRat(nl)
		into[s.ID] = &s
	}
	return mapErr(rows.Err())
}

package store

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type Electricity struct{ pool *pgxpool.Pool }

func NewElectricity(pool *pgxpool.Pool) *Electricity { return &Electricity{pool} }

func (s *Electricity) ListElectricityContracts(ctx context.Context) ([]model.ElectricityContract, error) {
	contracts, err := collect[model.ElectricityContract](s.pool.Query(ctx, `SELECT id, name FROM electricity_contracts ORDER BY id`))
	if err != nil {
		return nil, err
	}
	for i := range contracts {
		contracts[i].MeterIDs, err = collectIDs(ctx, s.pool, `SELECT meter_id FROM electricity_contract_meters WHERE contract_id=$1 ORDER BY meter_id`, contracts[i].ID)
		if err != nil {
			return nil, err
		}
		contracts[i].Prices, err = collect[model.ElectricityContractPrice](s.pool.Query(ctx, `SELECT id, valid_from, valid_to, t1, t2, t3 FROM electricity_contract_prices WHERE contract_id=$1 ORDER BY valid_from`, contracts[i].ID))
		if err != nil {
			return nil, err
		}
	}
	return contracts, nil
}

func collectIDs(ctx context.Context, pool *pgxpool.Pool, query string, args ...any) ([]int64, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, mapErr(err)
		}
		ids = append(ids, id)
	}
	return ids, mapErr(rows.Err())
}

func (s *Electricity) UpdateElectricityContract(ctx context.Context, id int64, in model.ElectricityContractUpdate) error {
	if len(in.MeterIDs) > 200 || len(in.Prices) > 200 {
		return invalid("settings: too many rows")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return mapErr(err)
	}
	defer rollback(ctx, tx)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM electricity_contracts WHERE id=$1)`, id).Scan(&exists); err != nil {
		return mapErr(err)
	}
	if !exists {
		return &Error{ErrNotFound, "not found"}
	}
	seen := map[int64]bool{}
	for _, meterID := range in.MeterIDs {
		if meterID <= 0 || seen[meterID] {
			return invalid("meter_ids: must be unique positive IDs")
		}
		seen[meterID] = true
		var ok bool
		err := tx.QueryRow(ctx, `SELECT resource='electricity' AND building_id IS NOT NULL AND premises_id IS NULL AND removed_at IS NULL FROM meters WHERE id=$1`, meterID).Scan(&ok)
		if err != nil {
			return mapErr(err)
		}
		if !ok {
			return invalid("meter_ids: must be active common electricity meters")
		}
		var assigned bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM electricity_contract_meters WHERE meter_id=$1 AND contract_id<>$2)`, meterID, id).Scan(&assigned); err != nil {
			return mapErr(err)
		}
		if assigned {
			return invalid("meter_ids: meter already belongs to another contract")
		}
	}
	for i, p := range in.Prices {
		if p.ValidFrom.IsZero() || (p.ValidTo != nil && p.ValidTo.Before(p.ValidFrom.Time)) || !validElectricityPrice(p.T1) {
			return invalid(fmt.Sprintf("prices[%d]: invalid period or t1 price", i))
		}
		if p.T2 != nil && !validElectricityPrice(*p.T2) {
			return invalid(fmt.Sprintf("prices[%d].t2: must be nonnegative with at most four decimals", i))
		}
		if p.T3 != nil && !validElectricityPrice(*p.T3) {
			return invalid(fmt.Sprintf("prices[%d].t3: must be nonnegative with at most four decimals", i))
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM electricity_contract_meters WHERE contract_id=$1`, id); err != nil {
		return mapErr(err)
	}
	for _, meterID := range in.MeterIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO electricity_contract_meters(contract_id,meter_id) VALUES($1,$2)`, id, meterID); err != nil {
			return mapErr(err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM electricity_contract_prices WHERE contract_id=$1`, id); err != nil {
		return mapErr(err)
	}
	for _, p := range in.Prices {
		if _, err := tx.Exec(ctx, `INSERT INTO electricity_contract_prices(contract_id,valid_from,valid_to,t1,t2,t3) VALUES($1,$2,$3,$4,$5,$6)`, id, p.ValidFrom, p.ValidTo, p.T1, p.T2, p.T3); err != nil {
			return mapErr(err)
		}
	}
	return mapErr(tx.Commit(ctx))
}

func validElectricityPrice(v float64) bool {
	return v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(math.Round(v*10000)/10000-v) < 1e-9
}

type electricityCalcRow struct {
	ContractID   int64    `db:"contract_id"`
	ContractName string   `db:"contract_name"`
	MeterID      int64    `db:"meter_id"`
	MeterName    string   `db:"meter_name"`
	Serial       string   `db:"serial_number"`
	Coefficient  float64  `db:"coefficient"`
	Tariffs      int      `db:"tariffs"`
	D1           *float64 `db:"delta_t1"`
	D2           *float64 `db:"delta_t2"`
	D3           *float64 `db:"delta_t3"`
	P1           *float64 `db:"price_t1"`
	P2           *float64 `db:"price_t2"`
	P3           *float64 `db:"price_t3"`
}

func (s *Electricity) CalculateElectricity(ctx context.Context, period model.Date) (model.ElectricityCalculation, error) {
	result := model.ElectricityCalculation{Period: period.Format("2006-01"), Complete: true, Contracts: []model.ElectricityContractResult{}}
	rows, err := collect[electricityCalcRow](s.pool.Query(ctx, `
SELECT c.id AS contract_id, c.name AS contract_name, m.id AS meter_id,
       COALESCE(m.name, m.serial_number) AS meter_name, m.serial_number, m.coefficient, m.tariffs,
       CASE WHEN r.id IS NULL THEN NULL ELSE (r.t1-COALESCE(prev.t1,m.initial_t1))*m.coefficient END AS delta_t1,
       CASE WHEN r.id IS NULL OR r.t2 IS NULL THEN NULL ELSE (r.t2-COALESCE(prev.t2,m.initial_t2))*m.coefficient END AS delta_t2,
       CASE WHEN r.id IS NULL OR r.t3 IS NULL THEN NULL ELSE (r.t3-COALESCE(prev.t3,m.initial_t3))*m.coefficient END AS delta_t3,
       p.t1 AS price_t1, p.t2 AS price_t2, p.t3 AS price_t3
FROM electricity_contracts c
LEFT JOIN electricity_contract_meters cm ON cm.contract_id=c.id
LEFT JOIN meters m ON m.id=cm.meter_id AND m.installed_at<=($1::date+interval '1 month'-interval '1 day')::date AND (m.removed_at IS NULL OR m.removed_at>=$1)
LEFT JOIN meter_readings r ON r.meter_id=m.id AND r.period=$1 AND r.deleted_at IS NULL
LEFT JOIN LATERAL (SELECT x.t1,x.t2,x.t3 FROM meter_readings x WHERE x.meter_id=m.id AND x.deleted_at IS NULL AND x.reading_date<r.reading_date ORDER BY x.reading_date DESC LIMIT 1) prev ON true
LEFT JOIN LATERAL (SELECT x.t1,x.t2,x.t3 FROM electricity_contract_prices x WHERE x.contract_id=c.id AND x.valid_from<=$1 AND (x.valid_to IS NULL OR x.valid_to>=$1) LIMIT 1) p ON true
ORDER BY c.id,m.id`, period))
	if err != nil {
		return result, err
	}
	byID := map[int64]int{}
	for _, row := range rows {
		ci, ok := byID[row.ContractID]
		if !ok {
			ci = len(result.Contracts)
			byID[row.ContractID] = ci
			result.Contracts = append(result.Contracts, model.ElectricityContractResult{ID: row.ContractID, Name: row.ContractName, Complete: true, Meters: []model.ElectricityMeterResult{}})
		}
		contract := &result.Contracts[ci]
		if row.MeterID == 0 {
			contract.Complete = false
			result.Complete = false
			continue
		}
		meter := calculateElectricityMeter(row)
		if meter.Status != "ok" {
			contract.Complete = false
			result.Complete = false
		} else {
			contract.Total += meter.Amount
		}
		contract.Meters = append(contract.Meters, meter)
	}
	for i := range result.Contracts {
		result.Contracts[i].Total = math.Round(result.Contracts[i].Total*100) / 100
		result.Total += result.Contracts[i].Total
		if !result.Contracts[i].Complete {
			result.Complete = false
		}
	}
	result.Total = math.Round(result.Total*100) / 100
	return result, nil
}

func calculateElectricityMeter(row electricityCalcRow) model.ElectricityMeterResult {
	meter := model.ElectricityMeterResult{MeterID: row.MeterID, Name: row.MeterName, Serial: row.Serial, Coefficient: row.Coefficient, Tariffs: row.Tariffs, DeltaT1: row.D1, DeltaT2: row.D2, DeltaT3: row.D3, PriceT1: row.P1, PriceT2: row.P2, PriceT3: row.P3, Status: "ok"}
	if row.D1 == nil || (row.Tariffs >= 2 && row.D2 == nil) || (row.Tariffs >= 3 && row.D3 == nil) {
		meter.Status = "no_reading"
		return meter
	}
	if row.P1 == nil || (row.Tariffs >= 2 && row.P2 == nil) || (row.Tariffs >= 3 && row.P3 == nil) {
		meter.Status = "no_tariff"
		return meter
	}
	meter.Amount = *row.D1 * *row.P1
	if row.Tariffs >= 2 {
		meter.Amount += *row.D2 * *row.P2
	}
	if row.Tariffs >= 3 {
		meter.Amount += *row.D3 * *row.P3
	}
	meter.Amount = math.Round(meter.Amount*100) / 100
	return meter
}

package store

import (
	"context"
	"math"
	"strings"

	"dom-backend/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Water struct{ pool *pgxpool.Pool }

func NewWater(pool *pgxpool.Pool) *Water { return &Water{pool} }

func (s *Water) ListWaterContracts(ctx context.Context) ([]model.WaterContract, error) {
	contracts, err := collect[model.WaterContract](s.pool.Query(ctx, `SELECT id,name,number,water_tariff_id,sewer_tariff_id,archived_at FROM water_contracts ORDER BY id`))
	if err != nil {
		return nil, err
	}
	for i := range contracts {
		contracts[i].MeterIDs, err = collectIDs(ctx, s.pool, `SELECT meter_id FROM water_contract_meters WHERE contract_id=$1 ORDER BY meter_id`, contracts[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return contracts, nil
}
func (s *Water) CreateWaterContract(ctx context.Context, in model.WaterContractUpdate) error {
	return s.write(ctx, 0, in)
}
func (s *Water) UpdateWaterContract(ctx context.Context, id int64, in model.WaterContractUpdate) error {
	return s.write(ctx, id, in)
}
func (s *Water) ArchiveWaterContract(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE water_contracts SET archived_at=(now() AT TIME ZONE 'Europe/Moscow')::date,updated_at=now() WHERE id=$1 AND archived_at IS NULL`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Water) write(ctx context.Context, id int64, in model.WaterContractUpdate) error {
	if err := in.Validate(); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return mapErr(err)
	}
	defer rollback(ctx, tx)
	if id != 0 {
		var locked int64
		if err := tx.QueryRow(ctx, `SELECT id FROM water_contracts WHERE id=$1 AND archived_at IS NULL FOR UPDATE`, id).Scan(&locked); err != nil {
			return mapErr(err)
		}
	}
	// Оба тарифа должны быть в м³ и относиться к одной организации.
	var valid bool
	if _, err := tx.Exec(ctx, `SELECT id FROM tariffs WHERE id IN($1,$2) ORDER BY id FOR SHARE`, in.WaterTariffID, in.SewerTariffID); err != nil {
		return mapErr(err)
	}
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tariffs w JOIN tariffs s ON s.id=$2
 WHERE w.id=$1 AND w.unit='m3' AND s.unit='m3' AND w.organization_id=s.organization_id
 AND w.deleted_at IS NULL AND s.deleted_at IS NULL)`, in.WaterTariffID, in.SewerTariffID).Scan(&valid)
	if err != nil {
		return mapErr(err)
	}
	if !valid {
		return invalid("tariff_ids: must be active m3 tariffs of the same organization")
	}
	// Блокировки не дают одновременно переназначить прибор или поменять единицу тарифа.

	var count int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT id FROM meters WHERE id=ANY($1::bigint[])
 AND resource='cold_water' AND building_id IS NOT NULL AND premises_id IS NULL AND deleted_at IS NULL ORDER BY id FOR UPDATE) locked`, in.MeterIDs).Scan(&count)
	if err != nil {
		return mapErr(err)
	}
	if count != len(in.MeterIDs) {
		return invalid("meter_ids: must be common cold water meters of the tariffs organization")
	}
	if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM meters m JOIN buildings b ON b.id=m.building_id
 JOIN tariffs t ON t.id=$2 WHERE m.id=ANY($1::bigint[]) AND b.organization_id<>t.organization_id)`, in.MeterIDs, in.WaterTariffID).Scan(&valid); err != nil {
		return mapErr(err)
	}
	if !valid {
		return invalid("meter_ids: must be common cold water meters of the tariffs organization")
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM water_contract_meters WHERE meter_id=ANY($1::bigint[]) AND contract_id<>$2)`, in.MeterIDs, id).Scan(&valid); err != nil {
		return mapErr(err)
	}
	if valid {
		return invalid("meter_ids: meter already belongs to another water contract")
	}
	if id == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO water_contracts(name,number,water_tariff_id,sewer_tariff_id) VALUES($1,$2,$3,$4) RETURNING id`, strings.TrimSpace(in.Name), in.Number, in.WaterTariffID, in.SewerTariffID).Scan(&id)
	} else {
		_, err = tx.Exec(ctx, `UPDATE water_contracts SET name=$2,number=$3,water_tariff_id=$4,sewer_tariff_id=$5,updated_at=now() WHERE id=$1`, id, strings.TrimSpace(in.Name), in.Number, in.WaterTariffID, in.SewerTariffID)
	}
	if err != nil {
		return mapErr(err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM water_contract_meters WHERE contract_id=$1`, id); err != nil {
		return mapErr(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO water_contract_meters(contract_id,meter_id) SELECT $1,unnest($2::bigint[])`, id, in.MeterIDs); err != nil {
		return mapErr(err)
	}
	return mapErr(tx.Commit(ctx))
}

type waterCalcRow struct {
	ContractID   int64  `db:"contract_id"`
	ContractName string `db:"contract_name"`
	model.WaterMeterResult
}

func (s *Water) CalculateWater(ctx context.Context, period model.Date) (model.WaterCalculation, error) {
	result := model.WaterCalculation{Period: period.Format("2006-01"), Contracts: []model.WaterContractResult{}}
	rows, err := collect[waterCalcRow](s.pool.Query(ctx, `
WITH readings AS (
 SELECT c.id contract_id,c.name contract_name,COALESCE(m.id,0) meter_id,
 COALESCE(m.name,m.serial_number,'') meter_name,COALESCE(m.serial_number,'') serial_number,
 r.t1 AS current,
 CASE WHEN prev.id IS NOT NULL THEN prev.t1 WHEN m.installed_at>=($1::date-interval '1 month')::date THEN m.initial_t1 END AS previous,
 CASE WHEN r.id IS NOT NULL AND (prev.period=($1::date-interval '1 month')::date
  OR (prev.id IS NULL AND m.installed_at>=($1::date-interval '1 month')::date))
 THEN (r.t1-COALESCE(prev.t1,m.initial_t1))*m.coefficient END AS volume,
 (SELECT price FROM tariff_prices p WHERE p.tariff_id=c.water_tariff_id AND p.deleted_at IS NULL AND p.valid_from<=$1 AND (p.valid_to IS NULL OR p.valid_to>=$1)) water_price,
 (SELECT price FROM tariff_prices p WHERE p.tariff_id=c.sewer_tariff_id AND p.deleted_at IS NULL AND p.valid_from<=$1 AND (p.valid_to IS NULL OR p.valid_to>=$1)) sewer_price
 FROM water_contracts c
 LEFT JOIN (water_contract_meters cm JOIN meters m ON m.id=cm.meter_id AND m.deleted_at IS NULL
  AND m.installed_at<=($1::date+interval '1 month'-interval '1 day')::date AND (m.removed_at IS NULL OR m.removed_at>=$1)) ON cm.contract_id=c.id
 LEFT JOIN meter_readings r ON r.meter_id=m.id AND r.period=$1 AND r.deleted_at IS NULL
 LEFT JOIN LATERAL (SELECT x.id,x.period,x.t1 FROM meter_readings x WHERE x.meter_id=m.id AND x.deleted_at IS NULL AND x.period<$1 ORDER BY x.period DESC LIMIT 1) prev ON true
 WHERE c.archived_at IS NULL OR c.archived_at>=$1
), amounts AS (
 SELECT *,CASE WHEN volume>=0 THEN COALESCE(round(volume*water_price,2),0) ELSE 0 END water_amount,
 CASE WHEN volume>=0 THEN COALESCE(round(volume*sewer_price,2),0) ELSE 0 END sewer_amount,
 CASE WHEN current IS NULL THEN 'no_reading' WHEN volume IS NULL THEN 'no_previous'
 WHEN volume<0 THEN 'negative' WHEN water_price IS NULL OR sewer_price IS NULL THEN 'no_tariff' ELSE 'ok' END status FROM readings
)
SELECT *,water_amount+sewer_amount amount FROM amounts ORDER BY contract_id,meter_id`, period))
	if err != nil {
		return result, err
	}
	result.Complete = len(rows) > 0
	byID := map[int64]int{}
	for _, row := range rows {
		ci, ok := byID[row.ContractID]
		if !ok {
			ci = len(result.Contracts)
			byID[row.ContractID] = ci
			result.Contracts = append(result.Contracts, model.WaterContractResult{ID: row.ContractID, Name: row.ContractName, Complete: true, Meters: []model.WaterMeterResult{}})
		}
		c := &result.Contracts[ci]
		if row.MeterID == 0 {
			c.Complete = false
			result.Complete = false
			continue
		}
		c.Meters = append(c.Meters, row.WaterMeterResult)
		if row.Status != "ok" {
			c.Complete = false
			result.Complete = false
		}
		if row.Volume != nil && *row.Volume >= 0 {
			c.Volume += *row.Volume
		}
		c.WaterAmount += row.WaterAmount
		c.SewerAmount += row.SewerAmount
	}
	for i := range result.Contracts {
		c := &result.Contracts[i]
		c.Volume = math.Round(c.Volume*1000) / 1000
		c.WaterAmount = math.Round(c.WaterAmount*100) / 100
		c.SewerAmount = math.Round(c.SewerAmount*100) / 100
		c.Total = math.Round((c.WaterAmount+c.SewerAmount)*100) / 100
		result.WaterAmount += c.WaterAmount
		result.SewerAmount += c.SewerAmount
	}
	result.WaterAmount = math.Round(result.WaterAmount*100) / 100
	result.SewerAmount = math.Round(result.SewerAmount*100) / 100
	result.Total = math.Round((result.WaterAmount+result.SewerAmount)*100) / 100
	return result, nil
}

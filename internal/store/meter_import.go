package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"dom-backend/internal/model"
)

// ImportReadings записывает показания на дату date (расчётный месяц period) одной транзакцией: при любой ошибке не сохраняется ничего.
// Помещение (квартира) ищется по номеру в доме, счётчик — по ресурсу и номеру. Если installedAt задан, неизвестные
// счётчики создаются в этом помещении с этой датой ввода и нулевыми начальными показаниями; иначе это ошибка строки.
// Показания записываются с источником import по тем же правилам, что и ручной ввод.
func (s *MeterReadings) ImportReadings(ctx context.Context, buildingID int64, resource string, date, period model.Date, installedAt *model.Date, rows []model.MeterReadingImportRow) (model.MeterReadingImportResult, error) {
	var res model.MeterReadingImportResult
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, mapErr(err)
	}
	defer rollback(ctx, tx)

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM buildings WHERE id = $1 AND deleted_at IS NULL)`, buildingID).Scan(&exists); err != nil {
		return res, mapErr(err)
	}
	if !exists {
		return res, &Error{ErrNotFound, "building not found"}
	}

	for _, r := range rows {
		if err := importReading(ctx, tx, buildingID, resource, date, period, installedAt, r, &res); err != nil {
			return model.MeterReadingImportResult{}, rowErr(r.Row, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.MeterReadingImportResult{}, mapErr(err)
	}
	return res, nil
}

func importReading(ctx context.Context, tx pgx.Tx, buildingID int64, resource string, date, period model.Date, installedAt *model.Date, r model.MeterReadingImportRow, res *model.MeterReadingImportResult) error {
	var premisesID int64
	err := tx.QueryRow(ctx,
		`SELECT id FROM premises WHERE building_id = $1 AND kind = 'apartment' AND number = $2 AND deleted_at IS NULL`,
		buildingID, r.Premises).Scan(&premisesID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return invalid(fmt.Sprintf("premises: apartment %q not found in this building", r.Premises))
		}
		return err
	}

	var meterID, meterPremises int64
	var tariffs int
	err = tx.QueryRow(ctx,
		`SELECT id, COALESCE(premises_id, 0), tariffs FROM meters WHERE resource = $1 AND serial_number = $2 AND deleted_at IS NULL`,
		resource, r.Serial).Scan(&meterID, &meterPremises, &tariffs)
	switch {
	case err == nil:
		if meterPremises != premisesID {
			return invalid(fmt.Sprintf("meter %q belongs to another premises", r.Serial))
		}
		if tariffs != r.Tariffs() {
			return invalid(fmt.Sprintf("meter %q has %d tariff(s), the row has %d reading(s)", r.Serial, tariffs, r.Tariffs()))
		}
	case errors.Is(err, pgx.ErrNoRows):
		if installedAt == nil {
			return invalid(fmt.Sprintf("meter %q not found", r.Serial))
		}
		err = tx.QueryRow(ctx,
			`INSERT INTO meters (premises_id, resource, tariffs, serial_number, installed_at) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			premisesID, resource, r.Tariffs(), r.Serial, *installedAt).Scan(&meterID)
		if err != nil {
			return err
		}
		res.MetersCreated++
	default:
		return err
	}

	in := model.MeterReading{MeterID: meterID, ReadingDate: date, Period: period, T1: *r.T[0], T2: r.T[1], T3: r.T[2], Source: "import"}
	if err := checkReading(ctx, tx, in, 0); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO meter_readings (meter_id, reading_date, period, t1, t2, t3, source) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		in.MeterID, in.ReadingDate, in.Period, in.T1, in.T2, in.T3, in.Source); err != nil {
		return err
	}
	res.Readings++
	return nil
}

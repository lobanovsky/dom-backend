package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"dom-backend/internal/model"
)

// ImportPulsarReadings импортирует переданные Пульсаром показания ХВС и ГВС.
// Пустые показания и проблемные строки сохраняются в журнале, но не блокируют остальные.
func (s *MeterReadings) ImportPulsarReadings(ctx context.Context, buildingID int64, readingDate model.Date, fileName string, parseErrors []model.ImportRowError, rows []model.PulsarReadingImportRow, _ bool) (model.MeterReadingImportResult, error) {
	var result model.MeterReadingImportResult
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, mapErr(err)
	}
	defer rollback(ctx, tx)

	var buildingExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM buildings WHERE id=$1 AND deleted_at IS NULL)`, buildingID).Scan(&buildingExists); err != nil {
		return result, mapErr(err)
	}
	if !buildingExists {
		return result, &Error{ErrNotFound, "building not found"}
	}
	period := readingDate.FirstOfMonth()
	var importRunID int64
	if err := tx.QueryRow(ctx, `INSERT INTO meter_reading_import_runs (building_id, resource, import_format, file_name, period, result, issues)
		VALUES ($1,'water','pulsar',$2,$3,'{}'::jsonb,'[]'::jsonb) RETURNING id`, buildingID, fileName, period).Scan(&importRunID); err != nil {
		return result, mapErr(err)
	}
	for _, rowErr := range parseErrors {
		result.Issues = append(result.Issues, model.MeterImportIssue{Row: rowErr.Row, Serial: rowErr.Serial, Kind: "invalid_row", Message: rowErr.Error})
	}

	meters := make(map[string]um31Meter)
	rowsDB, err := tx.Query(ctx, `SELECT m.id, m.serial_number, m.resource, m.tariffs, m.coefficient, m.premises_id, COALESCE(p.number,''), COALESCE(p.kind,'')
		FROM meters m LEFT JOIN premises p ON p.id=m.premises_id
		WHERE m.resource IN ('cold_water','hot_water') AND m.deleted_at IS NULL
		  AND (m.building_id=$1 OR p.building_id=$1)
		  AND m.installed_at <= CURRENT_DATE AND (m.removed_at IS NULL OR m.removed_at >= CURRENT_DATE)
		FOR UPDATE OF m`, buildingID)
	if err != nil {
		return result, mapErr(err)
	}
	for rowsDB.Next() {
		var m um31Meter
		var resource string
		if err := rowsDB.Scan(&m.ID, &m.Serial, &resource, &m.Tariffs, &m.Coefficient, &m.PremisesID, &m.PremisesNumber, &m.Kind); err != nil {
			rowsDB.Close()
			return result, mapErr(err)
		}
		m.Tariffs = 1
		meters[pulsarMeterKey(resource, m.Serial)] = m
	}
	if err := rowsDB.Err(); err != nil {
		rowsDB.Close()
		return result, mapErr(err)
	}
	rowsDB.Close()

	for _, row := range rows {
		key := pulsarMeterKey(row.Resource, row.Serial)
		m, ok := meters[key]
		if !ok {
			var belongs bool
			err := tx.QueryRow(ctx, `SELECT (m.building_id=$3 OR p.building_id=$3)
				FROM meters m LEFT JOIN premises p ON p.id=m.premises_id
				WHERE m.resource=$1 AND m.serial_number=$2 AND m.deleted_at IS NULL`, row.Resource, row.Serial, buildingID).Scan(&belongs)
			if err == pgx.ErrNoRows {
				result.Issues = append(result.Issues, model.MeterImportIssue{Row: row.Row, Serial: row.Serial, Kind: "unknown_meter", Message: "Счётчик с таким серийным номером не найден; строка пропущена."})
				continue
			}
			if err != nil {
				return result, mapErr(err)
			}
			if !belongs {
				result.Issues = append(result.Issues, model.MeterImportIssue{Row: row.Row, Serial: row.Serial, Kind: "another_building", Message: "Счётчик относится к другому дому; строка пропущена."})
				continue
			}
			result.Issues = append(result.Issues, model.MeterImportIssue{Row: row.Row, Serial: row.Serial, Kind: "meter_not_active", Message: "Счётчик выведен из эксплуатации или ещё не введён; строка пропущена."})
			continue
		}
		if row.Reading == nil {
			result.Issues = append(result.Issues, model.MeterImportIssue{Row: row.Row, Serial: row.Serial, Kind: "missing_reading", Message: "Пульсар не передал показание; строка пропущена."})
			continue
		}
		var already bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM meter_readings WHERE meter_id=$1 AND period=$2 AND deleted_at IS NULL)`, m.ID, period).Scan(&already); err != nil {
			return result, mapErr(err)
		}
		if already {
			result.SkippedExisting++
			result.Issues = append(result.Issues, model.MeterImportIssue{Row: row.Row, Serial: row.Serial, Kind: "already_imported", Message: "Показание за этот месяц уже внесено; существующее значение сохранено."})
			continue
		}
		previous, err := previousWaterReading(ctx, tx, m.ID, period)
		if err != nil {
			return result, mapErr(err)
		}
		value := *row.Reading
		var comment *string
		if model.Milli(value) < model.Milli(previous) {
			value = previous
			text := "Пульсар: значение ниже предыдущего; принято предыдущее показание, расход за период принят равным 0."
			comment = &text
			result.Issues = append(result.Issues, model.MeterImportIssue{Row: row.Row, Serial: row.Serial, Kind: "lower_reading_zero_consumption", Message: fmt.Sprintf("Показание %.3f м³ ниже предыдущего %.3f м³; записано предыдущее значение, расход 0.", *row.Reading, previous)})
		}
		in := model.MeterReading{MeterID: m.ID, ReadingDate: readingDate, Period: period, T1: value, Source: "import", Comment: comment}
		if err := insertPulsarReading(ctx, tx, in, importRunID); err != nil {
			if errors.Is(err, ErrInvalid) {
				result.Issues = append(result.Issues, model.MeterImportIssue{Row: row.Row, Serial: row.Serial, Kind: "invalid_reading", Message: err.Error()})
				continue
			}
			return result, rowErr(row.Row, err)
		}
		if warning, ok, err := highUsageWarning(ctx, tx, m, in, "м³"); err != nil {
			return result, mapErr(err)
		} else if ok {
			result.HighUsageWarnings = append(result.HighUsageWarnings, warning)
			result.Issues = append(result.Issues, model.MeterImportIssue{Row: row.Row, Serial: row.Serial, Kind: "high_usage", Message: fmt.Sprintf("Расход %.3f м³ больше чем втрое превышает прошлый месяц (%.3f м³).", warning.CurrentUsage, warning.PreviousUsage)})
		}
		result.Readings++
	}

	if err := saveMeterImportResult(ctx, tx, importRunID, result); err != nil {
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.MeterReadingImportResult{}, mapErr(err)
	}
	return result, nil
}

func pulsarMeterKey(resource, serial string) string { return resource + ":" + serial }

func previousWaterReading(ctx context.Context, tx pgx.Tx, meterID int64, period model.Date) (float64, error) {
	var previous float64
	err := tx.QueryRow(ctx, `SELECT t1 FROM meter_readings
		WHERE meter_id=$1 AND period<$2 AND deleted_at IS NULL ORDER BY period DESC LIMIT 1`, meterID, period).Scan(&previous)
	if err == pgx.ErrNoRows {
		if err := tx.QueryRow(ctx, `SELECT initial_t1 FROM meters WHERE id=$1`, meterID).Scan(&previous); err != nil {
			return 0, err
		}
		return previous, nil
	}
	return previous, err
}

func insertPulsarReading(ctx context.Context, tx pgx.Tx, in model.MeterReading, importRunID int64) error {
	if err := checkReading(ctx, tx, in, 0, false); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO meter_readings (meter_id, reading_date, period, t1, source, comment, import_run_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, in.MeterID, in.ReadingDate, in.Period, in.T1, in.Source, in.Comment, importRunID)
	return err
}

func saveMeterImportResult(ctx context.Context, tx pgx.Tx, importRunID int64, result model.MeterReadingImportResult) error {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}
	issuesJSON, err := json.Marshal(result.Issues)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE meter_reading_import_runs SET result=$2, issues=$3, reading_count=$4 WHERE id=$1`, importRunID, resultJSON, issuesJSON, result.Readings); err != nil {
		return mapErr(err)
	}
	return nil
}

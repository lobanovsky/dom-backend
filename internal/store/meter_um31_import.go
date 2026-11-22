package store

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"dom-backend/internal/model"
)

const um31EstimateCommentPrefix = "Расчётное показание по среднемесячному расходу"

type um31Meter struct {
	ID         int64
	Serial     string
	Tariffs    int
	PremisesID *int64
	Kind       string
}

type um31History struct {
	Period model.Date
	Values [3]*float64
}

// ImportUM31Readings загружает фактические показания УМ-31RTU и добавляет расчётные показания
// по среднему месячному расходу для действующих электросчётчиков без строки в файле.
func (s *MeterReadings) ImportUM31Readings(ctx context.Context, buildingID int64, averageMonths int, rows []model.UM31ReadingImportRow) (model.MeterReadingImportResult, error) {
	var result model.MeterReadingImportResult
	if averageMonths < 1 || averageMonths > 12 || len(rows) == 0 {
		return result, invalid("average_months: must be between 1 and 12")
	}
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

	period := rows[0].ReadingDate.FirstOfMonth()
	readingDate := rows[0].ReadingDate
	fileMeters := make(map[string]model.UM31ReadingImportRow, len(rows))
	for _, row := range rows {
		if row.ReadingDate.FirstOfMonth() != period {
			return result, rowErr(row.Row, invalid("all readings must be from one month"))
		}
		if row.ReadingDate.After(readingDate.Time) {
			readingDate = row.ReadingDate
		}
		fileMeters[row.Serial] = row
	}

	meters := make(map[string]um31Meter)
	rowsDB, err := tx.Query(ctx, `SELECT m.id, m.serial_number, m.tariffs, m.premises_id, COALESCE(p.kind,'')
		FROM meters m LEFT JOIN premises p ON p.id=m.premises_id
		WHERE m.resource='electricity' AND m.deleted_at IS NULL
		  AND (m.building_id=$1 OR p.building_id=$1)
		  AND m.installed_at <= CURRENT_DATE AND (m.removed_at IS NULL OR m.removed_at >= CURRENT_DATE)
		FOR UPDATE OF m`, buildingID)
	if err != nil {
		return result, mapErr(err)
	}
	for rowsDB.Next() {
		var m um31Meter
		if err := rowsDB.Scan(&m.ID, &m.Serial, &m.Tariffs, &m.PremisesID, &m.Kind); err != nil {
			rowsDB.Close()
			return result, mapErr(err)
		}
		meters[m.Serial] = m
	}
	if err := rowsDB.Err(); err != nil {
		rowsDB.Close()
		return result, mapErr(err)
	}
	rowsDB.Close()

	for _, row := range rows {
		m, ok := meters[row.Serial]
		if !ok {
			var belongsToBuilding bool
			err := tx.QueryRow(ctx, `SELECT (m.building_id=$2 OR p.building_id=$2)
				FROM meters m LEFT JOIN premises p ON p.id=m.premises_id
				WHERE m.resource='electricity' AND m.serial_number=$1 AND m.deleted_at IS NULL`, row.Serial, buildingID).Scan(&belongsToBuilding)
			if err == pgx.ErrNoRows {
				return result, rowErr(row.Row, invalid(fmt.Sprintf("meter %q not found", row.Serial)))
			}
			if err != nil {
				return result, mapErr(err)
			}
			if !belongsToBuilding {
				return result, rowErr(row.Row, invalid(fmt.Sprintf("meter %q belongs to another building", row.Serial)))
			}
			return result, rowErr(row.Row, invalid(fmt.Sprintf("meter %q is not in service", row.Serial)))
		}
		in, err := um31Input(m, row)
		if err != nil {
			return result, rowErr(row.Row, err)
		}
		var already bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM meter_readings WHERE meter_id=$1 AND period=$2 AND deleted_at IS NULL)`, m.ID, period).Scan(&already); err != nil {
			return result, mapErr(err)
		}
		if already {
			result.SkippedExisting++
			continue
		}
		if err := insertUM31Reading(ctx, tx, in); err != nil {
			return result, rowErr(row.Row, err)
		}
		result.Readings++
	}

	for _, m := range meters {
		if _, found := fileMeters[m.Serial]; found {
			continue
		}
		var already bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM meter_readings WHERE meter_id=$1 AND period=$2 AND deleted_at IS NULL)`, m.ID, period).Scan(&already); err != nil {
			return result, mapErr(err)
		}
		if already {
			result.SkippedExisting++
			continue
		}
		if m.PremisesID == nil || m.Kind != "apartment" {
			result.SkippedNoHistory++
			result.SkippedNoHistoryMeters = append(result.SkippedNoHistoryMeters, m.Serial)
			continue // усреднение по специальному порядку применяется только к квартирным счётчикам
		}
		in, ok, err := estimateUM31Reading(ctx, tx, m, readingDate, period, averageMonths)
		if err != nil {
			return result, mapErr(err)
		}
		if !ok {
			result.SkippedNoHistory++
			result.SkippedNoHistoryMeters = append(result.SkippedNoHistoryMeters, m.Serial)
			continue
		}
		if err := insertUM31Reading(ctx, tx, in); err != nil {
			return result, err
		}
		result.Readings++
		result.EstimatedReadings++
	}
	sort.Strings(result.SkippedNoHistoryMeters)

	if err := tx.Commit(ctx); err != nil {
		return model.MeterReadingImportResult{}, mapErr(err)
	}
	return result, nil
}

func um31Input(m um31Meter, row model.UM31ReadingImportRow) (model.MeterReading, error) {
	values := [3]*float64{}
	switch m.Tariffs {
	case 1:
		if row.Total != nil {
			values[0] = row.Total
		} else {
			values[0] = row.T[0]
		}
	case 2, 3:
		values = row.T
		if m.Tariffs == 2 {
			values[2] = nil
		}
	default:
		return model.MeterReading{}, invalid("meter has an unsupported number of tariffs")
	}
	for i := 0; i < m.Tariffs; i++ {
		if values[i] == nil {
			return model.MeterReading{}, invalid(fmt.Sprintf("A+%d reading is required for this meter", i+1))
		}
	}
	return model.MeterReading{MeterID: m.ID, ReadingDate: row.ReadingDate, Period: row.ReadingDate.FirstOfMonth(), T1: *values[0], T2: values[1], T3: values[2], Source: "import"}, nil
}

func insertUM31Reading(ctx context.Context, tx pgx.Tx, in model.MeterReading) error {
	var previousIsEstimate bool
	err := tx.QueryRow(ctx, `SELECT (source='import' AND COALESCE(comment,'') LIKE $2)
		FROM meter_readings WHERE meter_id=$1 AND deleted_at IS NULL AND reading_date<$3
		ORDER BY reading_date DESC LIMIT 1`, in.MeterID, um31EstimateCommentPrefix+"%", in.ReadingDate).Scan(&previousIsEstimate)
	if err != nil && err != pgx.ErrNoRows {
		return mapErr(err)
	}
	if previousIsEstimate {
		// При возобновлении передачи фактическое значение может оказаться ниже
		// расчётного. Сравниваем его с соседними фактическими значениями, игнорируя оценки.
		if err := checkReading(ctx, tx, in, 0, true); err != nil {
			return err
		}
		if err := checkUM31ActualNeighbours(ctx, tx, in); err != nil {
			return err
		}
	} else if err := checkReading(ctx, tx, in, 0, false); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO meter_readings (meter_id, reading_date, period, t1, t2, t3, source, comment)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, in.MeterID, in.ReadingDate, in.Period, in.T1, in.T2, in.T3, in.Source, in.Comment)
	return err
}

func checkUM31ActualNeighbours(ctx context.Context, tx pgx.Tx, in model.MeterReading) error {
	var tariffs int
	var initial [3]float64
	if err := tx.QueryRow(ctx, `SELECT tariffs, initial_t1, initial_t2, initial_t3 FROM meters WHERE id=$1 AND deleted_at IS NULL`, in.MeterID).Scan(&tariffs, &initial[0], &initial[1], &initial[2]); err != nil {
		return mapErr(err)
	}
	neighbour := func(op, order string) (values [3]*float64, found bool, err error) {
		err = tx.QueryRow(ctx, `SELECT t1,t2,t3 FROM meter_readings
			WHERE meter_id=$1 AND deleted_at IS NULL AND reading_date `+op+` $2
			AND NOT (source='import' AND COALESCE(comment,'') LIKE $3)
			ORDER BY reading_date `+order+` LIMIT 1`, in.MeterID, in.ReadingDate, um31EstimateCommentPrefix+"%").Scan(&values[0], &values[1], &values[2])
		if err == pgx.ErrNoRows {
			return values, false, nil
		}
		return values, err == nil, err
	}
	previous, hasPrevious, err := neighbour("<", "DESC")
	if err != nil {
		return mapErr(err)
	}
	if !hasPrevious {
		previous = [3]*float64{&initial[0], &initial[1], &initial[2]}
	}
	next, hasNext, err := neighbour(">", "ASC")
	if err != nil {
		return mapErr(err)
	}
	current := [3]*float64{&in.T1, in.T2, in.T3}
	for i := 0; i < tariffs; i++ {
		if previous[i] != nil && model.Milli(*current[i]) < model.Milli(*previous[i]) {
			return invalid(fmt.Sprintf("t%d: must not be less than previous actual reading", i+1))
		}
		if hasNext && next[i] != nil && model.Milli(*current[i]) > model.Milli(*next[i]) {
			return invalid(fmt.Sprintf("t%d: must not be greater than next actual reading", i+1))
		}
	}
	return nil
}

func estimateUM31Reading(ctx context.Context, tx pgx.Tx, m um31Meter, readingDate, targetPeriod model.Date, months int) (model.MeterReading, bool, error) {
	var latest um31History
	err := tx.QueryRow(ctx, `SELECT period, t1, t2, t3 FROM meter_readings
		WHERE meter_id=$1 AND deleted_at IS NULL AND period < $2
		ORDER BY period DESC LIMIT 1`, m.ID, targetPeriod).Scan(&latest.Period, &latest.Values[0], &latest.Values[1], &latest.Values[2])
	if err == pgx.ErrNoRows {
		return model.MeterReading{}, false, nil
	}
	if err != nil {
		return model.MeterReading{}, false, err
	}
	rows, err := tx.Query(ctx, `SELECT period, t1, t2, t3 FROM meter_readings
		WHERE meter_id=$1 AND deleted_at IS NULL AND period < $2
		  AND NOT (source='import' AND COALESCE(comment,'') LIKE $3)
		ORDER BY period DESC LIMIT $4`, m.ID, targetPeriod, um31EstimateCommentPrefix+"%", months+1)
	if err != nil {
		return model.MeterReading{}, false, err
	}
	var history []um31History
	for rows.Next() {
		var item um31History
		if err := rows.Scan(&item.Period, &item.Values[0], &item.Values[1], &item.Values[2]); err != nil {
			rows.Close()
			return model.MeterReading{}, false, err
		}
		history = append(history, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return model.MeterReading{}, false, err
	}
	rows.Close()
	if len(history) < 2 {
		return model.MeterReading{}, false, nil
	}
	// Запрос возвращает новые месяцы первыми. Усредняем расход каждого тарифа
	// отдельно, учитывая длину промежутка между фактическими показаниями.
	var total [3]float64
	var elapsed [3]int
	for i := 0; i+1 < len(history); i++ {
		newer, older := history[i], history[i+1]
		gap := monthDistance(older.Period.Time, newer.Period.Time)
		if gap < 1 || gap > months {
			continue
		}
		for tariff := 0; tariff < m.Tariffs; tariff++ {
			if newer.Values[tariff] == nil || older.Values[tariff] == nil {
				continue
			}
			delta := *newer.Values[tariff] - *older.Values[tariff]
			if delta < 0 {
				continue
			}
			total[tariff] += delta
			elapsed[tariff] += gap
		}
	}
	var average [3]float64
	for i := 0; i < m.Tariffs; i++ {
		if elapsed[i] == 0 {
			return model.MeterReading{}, false, nil
		}
		average[i] = total[i] / float64(elapsed[i])
	}
	values := [3]*float64{}
	for i := 0; i < m.Tariffs; i++ {
		if latest.Values[i] == nil {
			return model.MeterReading{}, false, nil
		}
		v := math.Round((*latest.Values[i]+average[i])*1000) / 1000
		values[i] = &v
	}
	comment := fmt.Sprintf("%s за %d мес.; фактическое показание отсутствует в выгрузке УМ-31RTU.", um31EstimateCommentPrefix, months)
	return model.MeterReading{MeterID: m.ID, ReadingDate: readingDate, Period: targetPeriod, T1: *values[0], T2: values[1], T3: values[2], Source: "import", Comment: &comment}, true, nil
}

func monthDistance(from, to time.Time) int {
	return (to.Year()-from.Year())*12 + int(to.Month()-from.Month())
}

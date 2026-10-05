package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

type Importer struct{ pool *pgxpool.Pool }

func NewImporter(pool *pgxpool.Pool) *Importer { return &Importer{pool} }

// Import создаёт помещения указанного вида, их собственников и по два лицевых счёта
// (ЖКУ и капремонт) одной транзакцией: при любой ошибке не сохраняется ничего.
// Физлица с одинаковым ФИО считаются одним человеком, в том числе уже имеющимся в БД.
// Собственник получает долю 1/1 и плательщика счетов с сегодняшней даты.
func (s *Importer) Import(ctx context.Context, buildingID int64, kind string, rows []model.ImportRow) (model.ImportResult, error) {
	var res model.ImportResult
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer rollback(ctx, tx)

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM buildings WHERE id = $1 AND deleted_at IS NULL)`, buildingID).Scan(&exists); err != nil {
		return res, mapErr(err)
	}
	if !exists {
		return res, &Error{ErrNotFound, "building not found"}
	}

	persons, err := loadPersonIDs(ctx, tx)
	if err != nil {
		return res, err
	}

	for _, r := range rows {
		if err := importRow(ctx, tx, buildingID, kind, r, persons, &res); err != nil {
			return model.ImportResult{}, rowErr(r.Row, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.ImportResult{}, mapErr(err)
	}
	return res, nil
}

func importRow(ctx context.Context, tx pgx.Tx, buildingID int64, kind string, r model.ImportRow, persons map[string]int64, res *model.ImportResult) error {
	var premisesID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO premises (building_id, kind, number, total_area, cadastral_number) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		buildingID, kind, r.Number, r.Area, nilIfEmpty(r.CadastralNumber)).Scan(&premisesID); err != nil {
		return err
	}
	res.Premises++

	key := personKey(r.LastName, r.FirstName, r.MiddleName)
	personID, ok := persons[key]
	if ok {
		res.PersonsReused++
	} else {
		if err := tx.QueryRow(ctx,
			`INSERT INTO persons (last_name, first_name, middle_name) VALUES ($1, $2, $3) RETURNING id`,
			r.LastName, r.FirstName, nilIfEmpty(r.MiddleName)).Scan(&personID); err != nil {
			return err
		}
		persons[key] = personID
		res.PersonsCreated++
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO ownerships (premises_id, person_id, share_num, share_den, valid_from) VALUES ($1, $2, 1, 1, CURRENT_DATE)`,
		premisesID, personID); err != nil {
		return err
	}
	res.Ownerships++

	for _, acc := range []struct{ number, purpose string }{
		{r.UtilitiesAccount, "utilities"},
		{r.CapitalRepairAccount, "capital_repair"},
	} {
		var accountID int64
		if err := tx.QueryRow(ctx,
			`INSERT INTO personal_accounts (number, premises_id, purpose, status, opened_at) VALUES ($1, $2, $3, 'active', CURRENT_DATE) RETURNING id`,
			acc.number, premisesID, acc.purpose).Scan(&accountID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO account_holders (account_id, person_id, valid_from) VALUES ($1, $2, CURRENT_DATE)`,
			accountID, personID); err != nil {
			return err
		}
		res.Accounts++
	}
	return nil
}

// loadPersonIDs возвращает действующих физлиц по ключу ФИО; при совпадении берётся запись с меньшим id.
// Сравнение в Go, а не в SQL: lower() в PostgreSQL зависит от локали БД и может не знать кириллицу.
func loadPersonIDs(ctx context.Context, tx pgx.Tx) (map[string]int64, error) {
	rows, err := tx.Query(ctx, `SELECT id, last_name, first_name, COALESCE(middle_name, '') FROM persons WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	ids := map[string]int64{}
	for rows.Next() {
		var id int64
		var last, first, middle string
		if err := rows.Scan(&id, &last, &first, &middle); err != nil {
			return nil, mapErr(err)
		}
		if k := personKey(last, first, middle); ids[k] == 0 {
			ids[k] = id
		}
	}
	return ids, mapErr(rows.Err())
}

func personKey(last, first, middle string) string {
	return strings.ToLower(strings.Join([]string{
		strings.Join(strings.Fields(last), " "),
		strings.Join(strings.Fields(first), " "),
		strings.Join(strings.Fields(middle), " "),
	}, "|"))
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// rowErr добавляет номер строки файла к сообщению, сохраняя вид ошибки (409/422/…).
func rowErr(row int, err error) error {
	err = mapErr(err)
	if e, ok := err.(*Error); ok {
		return &Error{e.Kind, fmt.Sprintf("row %d: %s", row, e.Msg)}
	}
	return err
}

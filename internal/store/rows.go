package store

import "github.com/jackc/pgx/v5"

// collect читает все строки результата в срез структур (по db-тегам).
func collect[T any](rows pgx.Rows, err error) ([]T, error) {
	if err != nil {
		return nil, mapErr(err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByName[T])
	if err != nil {
		return nil, mapErr(err)
	}
	if items == nil {
		items = []T{}
	}
	return items, nil
}

// one читает ровно одну строку; пустой результат — ErrNotFound.
func one[T any](rows pgx.Rows, err error) (T, error) {
	if err != nil {
		var zero T
		return zero, mapErr(err)
	}
	item, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[T])
	return item, mapErr(err)
}

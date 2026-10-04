package store

import "github.com/jackc/pgx/v5/pgconn"

// checkDeleted переводит результат DELETE в ошибку: ни одной строки — ErrNotFound.
func checkDeleted(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return &Error{ErrNotFound, "not found"}
	}
	return nil
}

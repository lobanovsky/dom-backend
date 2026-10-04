package store

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

// rollback откатывает транзакцию в defer. После Commit возвращается ErrTxClosed — это норма.
func rollback(ctx context.Context, tx pgx.Tx) {
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		slog.Error("rollback", "err", err)
	}
}

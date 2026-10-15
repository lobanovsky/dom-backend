package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
)

const userCols = `id, username, role, active, password_hash, token_version, created_at, updated_at, last_login_at, last_seen_at`

type Users struct{ pool *pgxpool.Pool }

func NewUsers(pool *pgxpool.Pool) *Users { return &Users{pool} }

func (s *Users) List(ctx context.Context) ([]model.User, error) {
	return collect[model.User](s.pool.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY lower(username)`))
}

func (s *Users) ByID(ctx context.Context, id int64) (model.User, error) {
	return one[model.User](s.pool.Query(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

// ByUsername ищет без учёта регистра.
func (s *Users) ByUsername(ctx context.Context, username string) (model.User, error) {
	return one[model.User](s.pool.Query(ctx, `SELECT `+userCols+` FROM users WHERE lower(username) = lower($1)`, username))
}

func (s *Users) Create(ctx context.Context, username, passwordHash, role string) (model.User, error) {
	return one[model.User](s.pool.Query(ctx,
		`INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3) RETURNING `+userCols, username, passwordHash, role))
}

// EnsureAdmin создаёт администратора, только если пользователей ещё нет (первый запуск); created=false — таблица не пуста.
func (s *Users) EnsureAdmin(ctx context.Context, username, passwordHash string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO users (username, password_hash, role)
		 SELECT $1, $2, 'admin' WHERE NOT EXISTS (SELECT 1 FROM users)`, username, passwordHash)
	if err != nil {
		return false, mapErr(err)
	}
	return tag.RowsAffected() == 1, nil
}

// Update меняет роль и активность. Последнего действующего администратора нельзя отключить или понизить.
// При отключении версия токена растёт, чтобы старые сессии не ожили после повторного включения.
func (s *Users) Update(ctx context.Context, id int64, role string, active bool) (model.User, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return model.User{}, mapErr(err)
	}
	defer rollback(ctx, tx)

	// Блокируем действующих админов: два одновременных понижения не оставят систему без администратора.
	rows, err := tx.Query(ctx, `SELECT id FROM users WHERE role = 'admin' AND active FOR UPDATE`)
	if err != nil {
		return model.User{}, mapErr(err)
	}
	admins, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return model.User{}, mapErr(err)
	}
	if role != model.RoleAdmin || !active {
		if len(admins) == 1 && admins[0] == id {
			return model.User{}, &Error{ErrConflict, "cannot remove the last admin"}
		}
	}
	u, err := one[model.User](tx.Query(ctx,
		`UPDATE users SET role = $2, active = $3, updated_at = now(),
		        token_version = token_version + CASE WHEN active AND NOT $3 THEN 1 ELSE 0 END
		 WHERE id = $1 RETURNING `+userCols, id, role, active))
	if err != nil {
		return model.User{}, err
	}
	return u, mapErr(tx.Commit(ctx))
}

// SetPassword меняет пароль и завершает все прежние сессии пользователя.
func (s *Users) SetPassword(ctx context.Context, id int64, passwordHash string) (model.User, error) {
	return one[model.User](s.pool.Query(ctx,
		`UPDATE users SET password_hash = $2, token_version = token_version + 1, updated_at = now()
		 WHERE id = $1 RETURNING `+userCols, id, passwordHash))
}

// RecordLogin пишет попытку входа; при успехе обновляет users.last_login_at.
func (s *Users) RecordLogin(ctx context.Context, e model.LoginEvent) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return mapErr(err)
	}
	defer rollback(ctx, tx)
	if _, err := tx.Exec(ctx,
		`INSERT INTO login_events (user_id, username, success, ip, user_agent) VALUES ($1, $2, $3, $4, $5)`,
		e.UserID, e.Username, e.Success, e.IP, e.UserAgent); err != nil {
		return mapErr(err)
	}
	if e.Success && e.UserID != nil {
		if _, err := tx.Exec(ctx, `UPDATE users SET last_login_at = now(), last_seen_at = now() WHERE id = $1`, *e.UserID); err != nil {
			return mapErr(err)
		}
	}
	return mapErr(tx.Commit(ctx))
}

// Touch отмечает активность пользователя (не меняет updated_at).
func (s *Users) Touch(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_seen_at = now() WHERE id = $1`, id)
	return mapErr(err)
}

// Logins — история входов, новые сверху; userID > 0 ограничивает одним пользователем.
func (s *Users) Logins(ctx context.Context, userID int64, limit, offset int) ([]model.LoginEvent, error) {
	return collect[model.LoginEvent](s.pool.Query(ctx,
		`SELECT id, user_id, username, success, ip, user_agent, created_at FROM login_events
		 WHERE $1 = 0 OR user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, userID, limit, offset))
}

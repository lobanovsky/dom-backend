# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`dom-backend` — Go monolith (REST API + PostgreSQL) for management companies (УК) and ТСН/ТСЖ in Moscow. The frontend lives in a sibling directory `../dom-frontend` (not started). Documentation (README, comments, commit messages) is in Russian; Go identifiers are English. `README.md` holds the ER diagram, API reference and CI/CD secrets, so keep it in sync when the model or API changes.

## Commands

```bash
cp .env.example .env && set -a; . ./.env; set +a   # env is NOT auto-loaded by the app
go run ./cmd/server                                # applies migrations on startup, listens on :8080
go run ./cmd/hashpw <password>                     # bcrypt hash for ADMIN_PASSWORD_HASH

gofmt -l .            # CI fails if this prints anything
go vet ./...
go test -race ./...
go test -race -run TestName ./internal/model       # single test

# store tests hit a real DB and are skipped without this var (they create and delete their own rows)
TEST_DATABASE_URL='postgres://dom:<pass>@localhost:5444/dom?sslmode=disable' go test -race ./internal/store

docker network create dom-network && docker compose up -d --build   # network is external, create once
```

Local dev DB: PostgreSQL on `localhost:5444`, database/user `dom`. Credentials live only in the untracked `.env`.

## Architecture

Layers, wired by hand in `cmd/server/main.go` (no DI framework): `httpapi` handler → `model.Validate()` → `store` (explicit pgx SQL).

- `internal/model` — entity structs (`json` + `db` tags), `Validate()`, `SetDefaults()`, `Date` type (`YYYY-MM-DD` in JSON, implements `Scan`/`Value` for pgx), per-entity `*Filter` structs. `Meta` (id, created_at, updated_at) is embedded in every entity and set by the server.
- `internal/store` — one file per entity, SQL written explicitly; reads use `collect`/`one` (`rows.go`, `pgx.RowToStructByName`, so a selected column list must match struct `db` tags exactly). `mapErr` (`errors.go`) turns PG error codes into `*store.Error` (23505→conflict/409, other 23xxx/22xxx→invalid/422).
- `internal/httpapi` — one file per entity: the store interface is declared **next to the handler** (consumer-side), so tests substitute fakes. `helpers.go` has `parseList` (rejects unknown query params), `decodeJSON` (rejects unknown body fields), `writeErr`. `router.go` takes everything through `Deps`; all `/api/v1/*` except `auth/login|logout` sit behind the cookie-session middleware (`auth.go`); `/healthz` is public.
- `internal/auth` — single admin from env (bcrypt hash + HMAC-signed token in cookie `dom_session`); no users table yet.
- `migrations/` — `NNNN_name.{up,down}.sql`, embedded and run via golang-migrate on every server start (`internal/db`).

Adding an entity: migration → model + `Validate()` → store file → httpapi file → field in `httpapi.Deps` + register in `NewRouter` → construct in `main.go` → README.

## Domain rules that span files

- **Account (лицевой счёт) belongs to premises, not to a person.** Payers are time-ranged rows in `account_holders`; one premises may have several accounts (`purpose`).
- **Owners are a person XOR a legal entity** (`ownerships`, `account_holders`): enforced both in `model.exactlyOne` and DB `CHECK`. Shares are `share_num/share_den` with a validity period.
- **Sum of ownership shares per premises must not exceed 1 at any date.** Not expressible as a DB constraint; `store.checkOwnershipShares` runs in the same transaction as the ownership insert/update (`Ownerships.write`) and rolls back on violation (→ 422).
- Validation is duplicated on purpose: `model.Validate()` gives field-level messages, DB constraints are the last line of defense.
- **Soft delete everywhere**: `DELETE` sets `deleted_at` (all 9 tables); stores never hard-delete. Lists return only active rows (`deleted_at IS NULL`), `?deleted=only` returns the trash; `Get` returns deleted rows too, `Update` ignores them (404); `POST /{resource}/{id}/restore` clears the mark. Integrity between active and deleted rows lives in DB triggers from migration `0003` (`assert_active`/`assert_no_active`, `RAISE EXCEPTION` → SQLSTATE `P0001` → `store.ErrInvalid` with the raw message): can't delete a row with active children, can't save/restore a row pointing at a deleted parent. Uniqueness (premises number, account number, cadastral, INN+KPP) is via partial unique indexes `WHERE deleted_at IS NULL` (index names kept = old constraint names, frontend maps them). Every SELECT/RETURNING must include `deleted_at` (`RowToStructByName`), every ownership-share query must ignore deleted rows. Store tests must clean up with hard `DELETE` (`cleanup(t, pool, table, id)`), not the store's `Delete`.
- `PUT` replaces the whole record (omitted optional fields become `null`); there is no `PATCH`.
- Defaults applied by handlers before `Validate()`: ownership share 1/1, residency `relation=other`, account `purpose=utilities`/`status=active`.

## CI/CD

`.github/workflows/build-and-deploy-backend.yml` (adapted from the author's `dr-notif-backend` project): test → publish image to Docker Hub → SSH deploy to the directory in secret `DEPLOY_HOST_PROJECT_PATH`, with `/healthz` check and rollback. Triggers on branch `master`. Production DB location is still undecided; secrets are listed in README. `ADMIN_PASSWORD_HASH` must have every `$` doubled (`$$`) in any `.env` read by docker compose.

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
- **Payments**: `bank_accounts` (organization's settlement accounts — not to be confused with `personal_accounts`; `active` is computed from `valid_from/valid_to`, never stored), `incoming_payments` / `outgoing_payments` (two tables on purpose, for filtering), `payment_registries` (uploaded Sber files with the original bytes and sha256), `payment_categories`. New CRUD resources use the generic `httpapi/crud.go` (`crudHandlers[T, F]` + `crudStore`) and `parseListSpec` (date/number/bool filters) instead of per-entity handler files. Registry upload (`httpapi/payment_registries.go` + `registry_files.go`) takes many `.txt` files and/or zip archives in one request; the bank account is derived from a 20-digit number in the file name (must match an existing `bank_accounts.number`), each file is its own transaction, macOS `__MACOSX`/`._` zip entries are skipped, zip limits guard against bombs. `store.PaymentRegistries.Import` dedups on two levels: file sha256 (409) and `UNIQUE (bank_account_id, external_id)` on payments — deliberately *without* `deleted_at IS NULL`, so a deleted payment stays reserved. Money is `NUMERIC(14,2)` ↔ `float64` in Go; the registry parser works in integer kopecks to compare with the summary line exactly. Parser tests use synthetic lines only; real registries live in gitignored `private/registry/`.
- **Bank statements** (`internal/clientbank`, `store.BankStatements.Import`, `httpapi/bank_statements.go`): the only supported format is the 1C client-bank exchange text file (`1CClientBankExchange` 1.03, UTF-8; Windows/DOS encodings are accepted when declared). Excel statements were dropped on purpose (too free-form, error-prone). Our accounts come from the file; one file may hold several accounts → one `ParsedStatement` per account that has documents (`Part` = account, the file key includes it when `MultiPart`). Direction: our account on the payer side = debit (`ДатаСписано`) → `outgoing_payments`, on the receiver side = credit (`ДатаПоступило`) → `incoming_payments`; a transfer between own accounts yields both legs. Per-account per-day section totals (`СекцияРасчСчет`) must equal the documents' sums, otherwise the whole file is rejected naming account and day (a hard error, not a warning: re-loading a corrected export would duplicate rows because the amount is part of `dedup_key`). Overlapping files are deduplicated by `dedup_key` (direction, date, doc number, amount, counterparty account, purpose, occurrence index) with `UNIQUE (bank_account_id, dedup_key)` including deleted rows; the same file by sha256. Statement files may be up to 50 MB (`maxStatementSize`), registries 5 MB. Uploads accept `.gz` (`inflate`, bomb-safe): the browser gzips `.txt` ≥ 256 KB (`lib/gzip.js`) because a proxy in front (Traefik default read timeout is 60 s) cut slow 9 MB uploads — it showed up as `status=400, ms≈60000, unexpected EOF` in the log (`upload failed`). No time of day in 1C, so `payment_time` is NULL for statement payments. Parser tests build synthetic files; the real exports live in gitignored `private/1c/`.
- **Rules / assignment** (`internal/rules` engine, `store.Assignments`, `store.PaymentRules`): `incoming_payments.assigned_by` (`registry|manual|rule`) records where a link came from; rules only ever touch unassigned payments and ones with `assigned_by='rule'`. Manual `PUT` that changes the link resets `assigned_by='manual'` and clears `rule_id/run_id` (SQL `CASE` in `IncomingPayments.Update`); registry import sets `registry`. `Assignments.Apply` runs in one tx, writes an `assignment_runs` row and per-payment previous state (`assignment_run_items`); `Rollback` restores only payments whose `run_id` still equals the run. A rule whose conditions match but whose action cannot resolve (number not found, ambiguous premises) does NOT stop processing — the next rule is tried; the first *substantive* reason is reported (`weak` reasons like «not found in text» lose). `link_by_owner` ignores `valid_from` of ownerships on purpose (imported data has valid_from = import date). Engine tests use a synthetic index; no DB.
  **Outgoing payments** use the same machinery (migration `0008`): `payment_rules.direction` / `assignment_runs.direction` are `incoming|outgoing`; outgoing rules only `set_category` (validated in `model.PaymentRule.Validate` per direction: `recipient_*` condition fields instead of `payer_*`, category must be of the same direction), `outgoing_payments` got `assigned_by (manual|rule)`, `rule_id`, `run_id`, and `OutgoingPayments.Update` resets them on a manual category change like the incoming SQL `CASE`. `rules.Payment` keeps its `Payer*` fields for both directions (counterparty); `fieldText` maps `recipient_*` onto them. `store.Assignments` picks table/queries by `req.Direction` (`loadOutgoingCandidates`, `updateAssignmentSQL`, rollback by the run's direction); `assignment_run_items.payment_id` has no FK any more because it points at either table. Rules are never mixed between directions (`loadRules` filters by direction).
- **Sber API** (`internal/sberapi`, `internal/sbersync`, `store.Sber`, `httpapi/sber.go`): read-only polling of statements from the bank instead of file uploads. `sberapi.Client` = mTLS (`.p12` via go-pkcs12 + CA dir) + OAuth2 refresh; the `refresh_token` rotates on every refresh, so the pair is persisted in `sber_tokens` right away (save failure after the bank issued a pair is an error). `sberapi.Statement` converts operations into a `model.ParsedStatement` and computes `dedup_key` with `clientbank.DedupKey`, so API and 1C-file operations never duplicate; `sbersync.Syncer` runs per account over a day range and feeds `BankStatements.Import` (`StatementExists/AllDuplicates` = «nothing new», not an error). `Start` runs in the background (HTTP returns 202; a long period would hit the proxy timeout), `Sync` blocks (scheduler). One run at a time via `pg_try_advisory_lock`; runs are logged in `sber_sync_runs`. Integration is off unless `SBER_CLIENT_ID` is set. Setup steps for the operator: `private/sber/instruction/README.md` (gitignored).
- `PUT` replaces the whole record (omitted optional fields become `null`); there is no `PATCH`.
- Defaults applied by handlers before `Validate()`: ownership share 1/1, residency `relation=other`, account `purpose=utilities`/`status=active`.

## Logging

`internal/logx` sets up slog JSON to stdout plus, when `LOG_FILE` is set, a lumberjack-rotated file; `docker-compose.yml` sets `LOG_FILE=/logs/dom-backend.log` and mounts `./logs` (the deploy script does `mkdir -p logs`), so logs survive container re-creation. `httpapi.logRequests` (wraps the whole router) logs one `request` line per call without query/body (names appear in search params) and skips `/healthz`. Import handlers log each file's status and failure reason (`registry file` / `statement file`) — validation failures are not HTTP errors, so without these lines they would leave no trace.

## CI/CD

`.github/workflows/build-and-deploy-backend.yml` (adapted from the author's `dr-notif-backend` project): test → publish image to Docker Hub → SSH deploy to the directory in secret `DEPLOY_HOST_PROJECT_PATH`, with `/healthz` check and rollback. Triggers on branch `master`. Production DB location is still undecided; secrets are listed in README. `ADMIN_PASSWORD_HASH` must have every `$` doubled (`$$`) in any `.env` read by docker compose.

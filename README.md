# dom-backend

Бэкенд приложения для управляющих компаний (УК) и ТСН/ТСЖ (Москва). Монолит на Go с REST API и PostgreSQL. Фронтенд появится позже, это отдельный каталог `dom-frontend`.

## Стек

- Go, стандартный `net/http` (`ServeMux` с паттернами Go 1.22+), `log/slog` (JSON)
- PostgreSQL, драйвер `pgx/v5`, SQL без ORM
- Миграции `golang-migrate`, встроены в бинарник (`embed`), применяются при старте
- Docker, GitHub Actions, деплой по SSH

## Доменная модель

```mermaid
erDiagram
    organizations ||--o{ buildings : "управляет"
    buildings ||--o{ premises : "содержит"
    premises ||--o{ ownerships : "принадлежит"
    persons ||--o{ ownerships : "владеет"
    legal_entities ||--o{ ownerships : "владеет"
    premises ||--o{ residencies : "проживают"
    persons ||--o{ residencies : "житель"
    persons |o--o{ residencies : "родственник собственника"
    premises ||--o{ personal_accounts : "лицевые счета"
    personal_accounts ||--o{ account_holders : "плательщики по периодам"
    persons |o--o{ account_holders : ""
    legal_entities |o--o{ account_holders : ""
```

| Таблица | Назначение |
|---|---|
| `organizations` | УК или ТСН/ТСЖ (`kind`: uk / tsn / tszh), ИНН, КПП, ОГРН |
| `buildings` | Дом: `apartment_building`, `parking`, `common_premises`, `other` |
| `premises` | Объект недвижимости: `apartment`, `non_residential`, `commercial`, `parking_space`, `storage`. Уникален по (дом, вид, номер) |
| `persons` | Физлица. Один человек может быть и собственником, и жителем |
| `legal_entities` | Юрлица-собственники |
| `ownerships` | Владение: помещение, владелец (физлицо **или** юрлицо), доля `share_num/share_den`, период, основание |
| `residencies` | Жители: прописан или фактически проживает, период, родство (`relation`, `related_owner_id`; родство необязательно) |
| `personal_accounts` | Лицевой счёт. Принадлежит **помещению**, у помещения может быть несколько счетов (`purpose`: utilities, capital_repair, parking, other) |
| `account_holders` | Плательщики счёта по периодам (физлицо **или** юрлицо) |

Правила, которые обеспечивает база:
- у владельца или плательщика заполнено ровно одно из `person_id` / `legal_entity_id`;
- доля не больше 1, конец периода не раньше начала;
- уникальны номер лицевого счёта, кадастровые номера и (дом, вид, номер) помещения.

Правило на уровне сервиса: на любую дату сумма долей по помещению не превышает 1 (`store.checkOwnershipShares`, в одной транзакции с записью). При нарушении API отвечает 422.

## Структура проекта

```
cmd/server        точка входа: конфиг, миграции, пул БД, ручная сборка зависимостей, HTTP-сервер, graceful shutdown
cmd/hashpw        утилита: bcrypt-хеш пароля администратора
internal/config   переменные окружения
internal/db       миграции и пул pgx
internal/model    структуры сущностей, перечисления, Validate() и SetDefaults(), тип Date, структуры фильтров
internal/store    по файлу на сущность: явный SQL на pgx, ошибки БД переводятся в store.Error
internal/auth     проверка пароля, подписанный токен сессии
internal/httpapi  по файлу на сущность: интерфейс хранилища и обработчики; helpers.go: разбор id, тела, фильтров, ошибок
migrations        SQL-миграции NNNN_name.{up,down}.sql
```

Слои: HTTP-обработчик разбирает запрос, вызывает `Validate()` модели и обращается к хранилищу через интерфейс, который описан рядом с обработчиком (в тестах хранилище подменяется фейком). Хранилище содержит весь SQL своей сущности. Ограничения БД остаются последним рубежом: нарушение превращается в 409 или 422.

Как добавить сущность: миграция, структура и `Validate()` в `internal/model`, файл в `internal/store`, файл в `internal/httpapi`, поле в `httpapi.Deps` и регистрация в `NewRouter`, сборка в `cmd/server/main.go`.

## Переменные окружения

| Переменная | Обязательна | По умолчанию | Описание |
|---|---|---|---|
| `DATABASE_URL` | да | | `postgres://user:pass@host:port/db?sslmode=disable` |
| `ADMIN_USERNAME` | да | | логин администратора |
| `ADMIN_PASSWORD_HASH` | да | | bcrypt-хеш (`go run ./cmd/hashpw <пароль>`). В `.env` для docker compose каждый `$` удваивается: `$$` |
| `ADMIN_SESSION_SECRET` | да | | секрет подписи cookie-сессии |
| `LISTEN_ADDR` | нет | `:8080` | адрес HTTP-сервера |
| `SESSION_TTL` | нет | `24h` | срок жизни сессии |

Шаблон: `.env.example`. Файл `.env` не коммитится.

## Локальный запуск

```bash
cp .env.example .env     # заполнить значения
set -a; . ./.env; set +a
go run ./cmd/server      # миграции применятся автоматически
```

Проверки: `gofmt -l .`, `go vet ./...`, `go test -race ./...`. Тесты хранилища работают с реальной БД и запускаются только при заданной `TEST_DATABASE_URL`, иначе пропускаются. Они создают свои записи и удаляют их в конце.

Через Docker:

```bash
docker network create dom-network   # один раз
docker compose up -d --build
```

Сеть `dom-network` внешняя: PostgreSQL должен быть подключён к ней, а в `DATABASE_URL` указано имя его контейнера.

## API

Все пути начинаются с `/api/v1`. Ошибки приходят как `{"error": "..."}`.

- `GET /healthz` без авторизации.
- `POST /api/v1/auth/login` с телом `{"username","password"}` ставит cookie `dom_session` (HttpOnly). `POST /api/v1/auth/logout` её сбрасывает. Остальные пути требуют cookie, иначе 401.

Ресурсы: `organizations`, `buildings`, `premises`, `persons`, `legal-entities`, `ownerships`, `residencies`, `accounts` (лицевые счета), `account-holders`.

| Метод и путь | Действие |
|---|---|
| `GET /{ресурс}?limit=&offset=&<фильтр>=` | список (`limit` по умолчанию 50, максимум 200). Ответ: `{"items":[...],"limit","offset"}`. Неизвестный параметр даёт 400 |
| `POST /{ресурс}` | создать, 201 |
| `GET /{ресурс}/{id}` | получить |
| `PUT /{ресурс}/{id}` | заменить запись целиком: не переданные необязательные поля станут `null` |
| `DELETE /{ресурс}/{id}` | удалить, 204 |
| `GET /premises/{id}/ownerships` | владения помещения с именем владельца (`owner_kind`, `owner_name`) |
| `GET /premises/{id}/accounts` | лицевые счета помещения с текущим плательщиком (`holder_name`) |

Фильтры списков:
- `organizations`: `kind`
- `buildings`: `organization_id`, `kind`
- `premises`: `building_id`, `kind`, `number`
- `persons`: `last_name`, `phone`
- `legal-entities`: `inn`
- `ownerships`: `premises_id`, `person_id`, `legal_entity_id`
- `residencies`: `premises_id`, `person_id`, `related_owner_id`
- `accounts`: `premises_id`, `number`, `status`, `purpose`
- `account-holders`: `account_id`, `person_id`, `legal_entity_id`

Значения по умолчанию при создании: у `ownerships` доля 1/1, у `residencies` `relation = other`, у `accounts` `purpose = utilities` и `status = active`. Даты передаются строкой `YYYY-MM-DD`. Поля `id`, `created_at`, `updated_at` выставляет сервер, а неизвестные поля в теле дают 400.

Коды ошибок: 400 неизвестное поле или параметр, неверный формат; 404 не найдено; 409 дубликат; 422 не прошла валидация (`{"error":"поле: причина"}`) или нарушено ограничение БД (внешний ключ, сумма долей); 401 нет сессии.

Пример:

```bash
curl -c cj -H 'Content-Type: application/json' -d '{"username":"admin","password":"..."}' localhost:8080/api/v1/auth/login
curl -b cj -H 'Content-Type: application/json' -d '{"kind":"tsn","name":"ТСН Пример"}' localhost:8080/api/v1/organizations
```

## CI/CD

Workflow `.github/workflows/build-and-deploy-backend.yml` запускается при PR и push в `master` и вручную:
1. `test`: `gofmt`, `go vet`, `go test -race`, сборка.
2. `publish` (push и ручной запуск): образ в Docker Hub с тегами `sha-<commit>` и `latest`.
3. `deploy`: по SSH на сервер, в каталог из секрета `DEPLOY_HOST_PROJECT_PATH`. Копирует `.env` и `docker-compose.yml`, делает `docker compose pull && up -d`, ждёт `/healthz`, при неудаче откатывает прошлую версию.

Секреты GitHub Actions:

| Секрет | Назначение |
|---|---|
| `DOCKER_USERNAME`, `DOCKER_TOKEN` | Docker Hub |
| `DEPLOY_HOST_IP`, `DEPLOY_HOST_PORT`, `DEPLOY_HOST_USERNAME`, `DEPLOY_HOST_KEY` | SSH-доступ к серверу |
| `DEPLOY_HOST_PROJECT_PATH` | каталог проекта на сервере |
| `DATABASE_URL` | строка подключения боевой БД |
| `ADMIN_USERNAME`, `ADMIN_PASSWORD_HASH`, `ADMIN_SESSION_SECRET` | вход администратора |

## Планы

Роли и пользователи в БД (администратор УК, председатель ТСН, собственник), начисления и платежи, общие собрания (кворум по долям), фронтенд.

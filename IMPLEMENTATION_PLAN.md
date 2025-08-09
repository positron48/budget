## Пошаговый план реализации Budget (Go + gRPC + PostgreSQL)

Ниже — детальный план, разбитый на этапы, с командами для проверки каждого этапа. Архитектура следует MVC: gRPC handlers = контроллеры (валидация/сериализация), application services = сервисы (бизнес‑логика), postgres adapters = репозитории (SQL без бизнес‑логики). Мультивалютность и многотенантность учтены.

### Текущий статус (сделано)

- Базовое окружение (Docker Compose: db/app/envoy), минимальный gRPC‑сервер, health.
- Миграции PostgreSQL: tenants/users/user_tenants/refresh_tokens, categories/category_i18n, transactions (с fx snapshot), fx_rates.
- Конфиг/логгер/pgx pool.
- Auth: Argon2id хешер, JWT issuer, репозитории, usecase с `Register/Login/Refresh`, gRPC‑хендлеры включены.
- Tenant/Category: домены, usecase, репозитории (PostgreSQL), gRPC‑хендлеры включены.
- FxRepo/tenant GetByID: для конвертации сумм в базовую валюту (TransactionService).
- Интерсепторы: Auth/Logging/Recovery/TenantGuard подключены в `cmd/budgetd/main.go`.
- gRPC‑серверы для всех сервисов зарегистрированы в main (Auth/Tenant/Category/Transaction/Report/Fx/User/Import(скелет)).
- Покрытие тестами ≥80% почти во всех модулях (см. раздел «Дорожная карта со статусами»).
- Локальная проверка `make check` зелёная; настроены buf lint и golangci‑lint (docker).

### Дорожная карта со статусами

- ✅ Базовое окружение (Docker Compose, Envoy), миграции, конфиг/логгер — сделано.
- ✅ gRPC интерсепторы (Auth, Logging, Recovery, TenantGuard) — реализованы и включены в `cmd/budgetd/main.go`.
- ✅ gRPC серверы: Auth, Tenant, Category, Transaction, Report, Fx, User — реализованы; Import — скелет.
- ✅ Репозитории: user, tenant, category, transaction, fx, refresh_token — реализованы.
- ✅ Usecase‑сервисы: auth, tenant, category, transaction, report, user — реализованы.
- ✅ Интеграционные gRPC‑тесты — сделано: есть тесты Category с аутентификацией, реализован сценарий Auth (Register→Login→Refresh→access), добавлены сценарии Transaction+Report и Fx (Upsert/Get/Batch), добавлен Tenant (Create/List). 
- 🟡 CI — частично: локальный `make check` зелёный; добавить GitHub Actions (buf generate, тесты, линт).
- 🟡 Observability — частично: логирование/Recovery есть; OpenTelemetry/Prometheus — TODO.
- 🟡 ImportService — частично: есть скелет `ImportServer`, требуется реализация и тесты.

Покрытие тестами (сейчас):
- internal/adapter/grpc: 88.9% — ок (перевыполнено, добавлены интеграции и расширенные unit‑тесты).
- internal/usecase/transaction: 86.1% — ок.
- internal/usecase/report: 85.5% — ок.
- internal/adapter/auth: 84.2% — ок.
- internal/usecase/user: 83.3% — ок.
- internal/pkg/config/logger/ctxutil: 87.5–100% — ок.
- internal/usecase/category/tenant: 100% — ок.

### 0. Предварительные требования (локально)

- Go 1.23+
- protoc >= 3.21, `buf` (buf.build)
- Docker, Docker Compose v2
- `golang-migrate` CLI
- `grpcurl`, `ghz` (нагрузочное тестирование — опционально)
- `make` (или `just`) для удобства

Проверка:
```bash
go version
protoc --version
buf --version || true # или используйте make dproto
docker --version && docker compose version
```

### 1. Структура репозитория и протосхемы

- Структура — см. `README.md` (cmd, internal/{domain,usecase,adapter,pkg}, migrations, proto, gen, web).
- Протосхемы уже добавлены в `proto/budget/v1/*.proto` (Auth, User, Tenant, Category, Transaction, Report, Import, Fx + common).

Проверка:
```bash
cd proto
buf lint
buf generate
```
Ожидается генерация stubs в `gen/go` и `gen/ts` (сконфигурировано в `buf.gen.yaml`).

### 2. Docker окружение (dev)

Создать `docker-compose.yml` и конфиги Envoy.

```yaml
version: "3.9"
services:
  db:
    image: postgres:15
    environment:
      POSTGRES_DB: budget
      POSTGRES_USER: budget
      POSTGRES_PASSWORD: budget
    ports:
      - "5432:5432"
    volumes:
      - db_data:/var/lib/postgresql/data

  envoy:
    image: envoyproxy/envoy:v1.31.0
    ports:
      - "8081:8081" # gRPC-Web
    volumes:
      - ./deploy/envoy.yaml:/etc/envoy/envoy.yaml:ro
    depends_on:
      - app

  app:
    build:
      context: .
      dockerfile: deploy/Dockerfile
    environment:
      APP_ENV: dev
      GRPC_ADDR: 0.0.0.0:8080
      DATABASE_URL: postgres://budget:budget@db:5432/budget?sslmode=disable
      JWT_ACCESS_TTL: 15m
      JWT_REFRESH_TTL: 720h
      JWT_SIGN_KEY: dev-secret-change
    ports:
      - "8080:8080" # gRPC
    depends_on:
      - db

volumes:
  db_data:
```

`deploy/envoy.yaml` (минимум для gRPC-Web → gRPC):
```yaml
static_resources:
  listeners:
  - name: listener_0
    address:
      socket_address: { address: 0.0.0.0, port_value: 8081 }
    filter_chains:
    - filters:
      - name: envoy.filters.network.http_connection_manager
        typed_config:
          "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
          stat_prefix: ingress_http
          codec_type: AUTO
          route_config:
            name: local_route
            virtual_hosts:
            - name: local_service
              domains: ["*"]
              routes:
              - match: { prefix: "/" }
                route: { cluster: app, timeout: 0s }
          http_filters:
          - name: envoy.filters.http.grpc_web
          - name: envoy.filters.http.cors
          - name: envoy.filters.http.router
  clusters:
  - name: app
    connect_timeout: 0.25s
    type: logical_dns
    lb_policy: round_robin
    http2_protocol_options: {}
    load_assignment:
      cluster_name: app
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address: { address: app, port_value: 8080 }
```

Проверка:
```bash
docker compose up -d --build
docker compose ps
```

Альтернатива: локальный запуск без контейнеров

```bash
make build
GRPC_ADDR=0.0.0.0:8080 DATABASE_URL=postgres://budget:budget@localhost:5432/budget?sslmode=disable ./bin/budgetd
```

### 3. Go-модуль и зависимости

```bash
go mod init github.com/positron48/budget
go get google.golang.org/grpc@latest
go get google.golang.org/protobuf@latest
go get github.com/jackc/pgx/v5@latest
go get go.uber.org/zap@latest
go get github.com/golang-jwt/jwt/v5@latest
go get golang.org/x/crypto@latest
go get github.com/golang-migrate/migrate/v4@latest
```

Создать `cmd/budgetd/main.go`, конфиг, логгер, bootstrap (wire/manual DI):
- Инициализация: config → logger → DB pool (pgx) → репозитории → сервисы → gRPC сервер + interceptors.
- Интерсепторы: logging, recovery, auth (JWT), tenant extraction (metadata `x-tenant-id` или default).

Проверка: сборка и запуск локально `go run ./cmd/budgetd` (пока без логики RPC).

### 4. Миграции БД (PostgreSQL)

Создать SQL‑миграции в `migrations`:
- 0001_init_tenants_users.sql: tenants, users, user_tenants, refresh_tokens
- 0002_categories.sql: categories, category_i18n
- 0003_transactions.sql: transactions (amount, currency, base_amount, base_currency, fx_rate, fx_provider, fx_as_of, occurred_at, comment, created_at)
- 0004_fx_rates.sql: fx_rates (from_ccy, to_ccy, rate, as_of, provider, UNIQUE)
- Индексы: по `tenant_id`, датам, категориям; уникальности (tenant_id, kind, code)
- (опционально) политики RLS

Проверка:
```bash
migrate -database "postgres://budget:budget@localhost:5432/budget?sslmode=disable" -path migrations up
```

### 5. Слой domain/usecase (чистая архитектура)

- `internal/domain`: сущности (`User`, `Tenant`, `Category`, `Transaction`, `Money`, ошибки домена)
- `internal/usecase`: интерфейсы портов (репозитории), сервисы (AuthService, TenantService, CategoryService, TransactionService, ReportService, FxService)

Критерии:
- Нет зависимостей от gRPC/DB в domain/usecase.

Статус: ✅ сделано (сервисы и интерфейсы реализованы; тесты покрывают бизнес‑логику и конвертацию валют).

### 6. Репозитории (PostgreSQL adapters)

- `internal/adapter/postgres`: реализация интерфейсов репозиториев на `pgx`.
- Транзакции/Unit of Work для согласованных операций (create user + tenant и т.п.).

Тесты:
- Интеграционные тесты с Dockerized PostgreSQL (через `docker compose` profile `test` или `testcontainers-go`).
- Покрыть CRUD по категориям, транзакции (с сохранением base_amount и fx_*), fx_rates CRUD.

Статус: 🟡 частично — репозитории реализованы; добавить интеграционные тесты с реальной БД.

### 7. Аутентификация и авторизация

- Пароли: Argon2id (salt per user, параметры cost в конфиге).
- JWT: HS256 (dev)/RS256 (prod), `sub`, `tenant_id`, `exp`, `iat`.
- Refresh токены: хранение/ротация в БД, привязка к пользователю/девайсу.
- Интерсептор auth: извлекать Bearer, валидировать, помещать `user_id`, `tenant_id` в контекст.

Тесты:
- Unit: пароль/хеширование, генерация/валидация JWT.
- Интеграция: Register → Login → Refresh → доступ к защищенному RPC.

Статус: 🟡 частично — unit‑тесты есть (argon2, jwt issuer, интерсептор auth); интеграционный сценарий нужно расширить.

### 8. gRPC handlers (контроллеры)

- `internal/adapter/grpc`: реализация серверов для всех сервисов из proto. Контроллеры делают:
  - валидацию входа
  - извлечение контекста пользователя/тенанта
  - вызовы application сервисов
  - маппинг доменных объектов в protobuf

Тесты:
- Интеграционные gRPC‑тесты (in‑process сервер) на Auth/User/Tenant/Category/Transaction/Report/Fx.

Статус: 🟡 частично — реализованы все серверы; unit‑тесты есть; интеграцию расширить (есть базовая проверка Category с auth).

Требуется доделать:
- Добавить интеграционные сценарии для Auth (полный Register→Login→Refresh), Tenant (ListMyTenants), Transaction (Create/Update/List), Report (MonthlySummary), Fx (Upsert/Get/BatchGet) с цепочкой интерсепторов.

### 9. CategoryService (CRUD + i18n)

- Правила: уникальность (tenant_id, kind, code), soft delete через `is_active`.
- i18n: `category_i18n` с выбором по locale, fallback.

Проверка:
```bash
grpcurl -plaintext -H "authorization: Bearer <ACCESS>" -H "x-tenant-id: <TENANT>" \
  -d '{"kind":"CATEGORY_KIND_EXPENSE","code":"food","translations":[{"locale":"ru","name":"Еда"}]}' \
  localhost:8080 budget.v1.CategoryService/CreateCategory
```

Статус: ✅ сделано — CRUD с валидацией, i18n и приоритизацией locale; unit‑тесты покрывают позитив/негативные сценарии.

### 10. FxService

- Таблица `fx_rates` и кэш в памяти (LRU, TTL).
- RPC: GetRate, UpsertRate, BatchGetRates.
- Коннектор к внешнему провайдеру (позже): абстракция провайдера (порт), адаптер для CBR/ECB.

Тесты: unit (конвертация, выбор ближайшей даты), интеграция (персистентность).

Статус: ✅ сделано — сервер/репозиторий реализованы, unit‑тесты покрывают основные случаи; интеграционные сценарии планируются.

### 11. TransactionService

- Create:
  - Валидация: категория принадлежит тенанту, type совпадает с kind.
  - Получить FX (amount.currency → tenant.base_currency) на `occurred_at::date` (или ближайший предыдущий день).
  - Рассчитать `base_amount` и заполнить `fx` (rate/provider/as_of), далее сохранить запись.
- Update:
  - При изменении `amount`, `occurred_at` или валюты — пересчитать `base_amount`.
- List/Get/Delete: стандартно; в List фильтры по дате/категориям/типу/валюте/поиску.

Проверка (пример):
Статус: ✅ сделано — usecase реализован (пересчет base_amount при изменениях), сервер обновляет по field mask; unit‑тесты закрывают позитив/негатив сценарии.
```bash
# Создание транзакции в USD при базе RUB
grpcurl -plaintext -H "authorization: Bearer <ACCESS>" -H "x-tenant-id: <TENANT>" \
  -d '{"type":"TRANSACTION_TYPE_EXPENSE","category_id":"<CAT>","amount":{"currency_code":"USD","minor_units":10000},"occurred_at":"2025-02-03T12:00:00Z","comment":"Books"}' \
  localhost:8080 budget.v1.TransactionService/CreateTransaction
```

### 12. ReportService

- Месячная сводка: агрегирование по категориям.
- Суммирование в целевой валюте:
  - если целевая = базовая — суммировать `base_amount` по транзакциям месяца.
  - если целевая ≠ базовая — для каждой транзакции (или по дням) конвертировать `base_amount` базовой валюты в целевую по курсу `base→target` на `occurred_at::date`, затем агрегировать.

Тесты: интеграционные тесты агрегатов на смешанных валютах.

Статус: ✅ сделано — usecase реализован, unit‑тесты есть; интеграционные сценарии начаты.

### 13. UserService, TenantService

- User: GetMe/UpdateProfile/ChangePassword.
- Tenant: CreateTenant/ListMyTenants (default tenant для новых пользователей).

Тесты: сценарии смены профиля, проверки ролей/доступа.

Статус: ✅ сделано — сервер/сервис реализованы; unit‑тесты на основные операции присутствуют; интеграционные тесты Tenant будут добавлены.

### 14. ImportService (скелет)

- Потоковая загрузка CSV (chunking), сохранение временного файла (локально или S3‑совместимое хранилище), настройка маппинга колонок, предпросмотр, commit.
- На этапе commit — батч‑вставка транзакций через сервисный слой (с учетом FX).

Тесты: e2e на небольших CSV.

Статус: 🟡 частично — сервер‑скелет создан; требуется реализовать сценарий импорта и покрыть тестами.

### 15. Обсервабилити и инфраструктура

- Логи: Zap с корреляцией по request id.
- Метрики/трейсы: OpenTelemetry (экспортер Prometheus/OTLP), базовые метрики RPC и БД.
- Health checks: gRPC Health Checking Protocol, `/healthz` в app (опционально http‑probe для k8s).

Проверка: `docker compose logs -f`, метрики на `/metrics` (если включены http‑эндпоинтом).

Статус: 🟡 частично — логирование и recovery есть; добавлен MetricsUnaryInterceptor и /metrics сервер (Prometheus). Трейсы OTel — TODO.

### 16. Lint/CI/CD

- Линтеры: `buf lint`, `go vet`, `golangci-lint` (локально через docker образ).
- GitHub Actions:
  - job: proto lint + generate
  - job: go build + test + lint (Go 1.23.x, golangci-lint 1.64.8)
  - job: docker build/push (опционально)

Статус: 🟡 частично — локальный `make check` зелёный; добавлен базовый CI GitHub Actions (buf lint/generate, golangci-lint, go test + coverage≥80%).

План доработки CI:
- Добавить job `proto` (buf lint + buf generate) с кэшированием `gen/go`.
- Добавить job `go` (build + test + coverage + golangci-lint).
- Порог покрытий: не ниже 80% по пакетам core.

### 17. Makefile цели (рекомендация)

```make
PROTO_DIR=proto

.PHONY: proto
proto:
	cd $(PROTO_DIR) && buf lint && buf generate

.PHONY: dproto
dproto:
	docker run --rm -v $(PWD):/workspace -w /workspace/proto bufbuild/buf:latest generate

.PHONY: build
build:
	go build -o bin/budgetd ./cmd/budgetd

.PHONY: test
test:
	go test ./...

.PHONY: fmt
fmt:
	gofumpt -w . || true
	gofmt -s -w .

.PHONY: lint
lint:
	docker run --rm -e GOTOOLCHAIN=local -v $(PWD):/app -w /app golangci/golangci-lint:v1.64.8 golangci-lint run --timeout=5m

.PHONY: check
check: tidy fmt vet lint test

.PHONY: migrate-up
migrate-up:
	migrate -database "postgres://budget:budget@localhost:5432/budget?sslmode=disable" -path migrations up

.PHONY: migrate-down
migrate-down:
	migrate -database "postgres://budget:budget@localhost:5432/budget?sslmode=disable" -path migrations down 1

.PHONY: run
run:
	go run ./cmd/budgetd
```

### 18. Контрольные точки и критерии готовности

- Этап A (Env): Docker окружение поднимается, PostgreSQL доступен, Envoy слушает 8081.
- Этап B (Proto): `buf lint`/`buf generate` зеленые, клиенты генерируются.
- Этап C (DB): миграции применяются без ошибок, таблицы созданы, индексы на месте.
- Этап D (Auth): Register/Login/Refresh работают, токены валидируются интерсептором.
- Этап E (Tenant/Category): CRUD с валидацией и i18n.
- Этап F (Fx): Get/Upsert/Batch rates, уникальность по from/to/as_of/provider.
- Этап G (Transaction): Create пересчитывает `base_amount`, List/Get/Delete работают.
- Этап H (Report): сводка корректна для смешанных валют, `target_currency_code` учитывается.
- Этап I (Import): upload → mapping → preview → commit (мини‑сценарий на 5–10 строк).
- Этап J (Tests): unit+integration покрывают основные сценарии, CI зелёный.

Статус: 🟡 частично — unit покрытие высокое (80%+ почти везде); интеграционные сценарии и CI ещё дополняются.

### 19. E2E smoke‑тесты (grpcurl)

1) Регистрация и логин
```bash
grpcurl -plaintext -d '{"email":"u@example.com","password":"Passw0rd!","name":"U","locale":"ru","tenant_name":"Дом"}' localhost:8080 budget.v1.AuthService/Register
grpcurl -plaintext -d '{"email":"u@example.com","password":"Passw0rd!"}' localhost:8080 budget.v1.AuthService/Login
```

2) Создание категории и транзакции
```bash
grpcurl -plaintext -H "authorization: Bearer <ACCESS>" -H "x-tenant-id: <TENANT>" \
  -d '{"kind":"CATEGORY_KIND_EXPENSE","code":"food","translations":[{"locale":"ru","name":"Еда"}]}' \
  localhost:8080 budget.v1.CategoryService/CreateCategory

grpcurl -plaintext -H "authorization: Bearer <ACCESS>" -H "x-tenant-id: <TENANT>" \
  -d '{"type":"TRANSACTION_TYPE_EXPENSE","category_id":"<CAT>","amount":{"currency_code":"USD","minor_units":10000},"occurred_at":"2025-02-03T12:00:00Z","comment":"Books"}' \
  localhost:8080 budget.v1.TransactionService/CreateTransaction
```

3) Отчет за месяц
```bash
grpcurl -plaintext -H "authorization: Bearer <ACCESS>" -H "x-tenant-id: <TENANT>" \
  -d '{"year":2025,"month":2,"locale":"ru","target_currency_code":"RUB"}' \
  localhost:8080 budget.v1.ReportService/GetMonthlySummary
```

---

Примечания по дизайну мультивалютности:
- В транзакции сохраняем оригинал (`amount`) и рассчитанный `base_amount` в базовой валюте тенанта на дату транзакции, плюс `fx` (rate/provider/as_of).
- Курсы храним в `fx_rates` и используем при добавлении/обновлении транзакций и в отчетах.
- В отчетах при целевой валюте, отличной от базовой, выполняется конвертация `base→target` по курсу на день транзакции (или агрегируется по дням для ускорения).



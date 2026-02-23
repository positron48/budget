# AGENTS.md — Budget

Краткая справка для быстрого погружения в проект.

## Что это
Многопользовательское веб‑приложение для учета доходов и расходов. Состоит из Go‑бэкенда (gRPC API) и веб‑клиента на Next.js. Поддерживает роли, организации (multi‑tenant), импорт/экспорт CSV, отчеты и i18n (RU/EN).

## Как работает (в двух словах)
- Веб‑клиент общается с бэкендом по gRPC‑Web через прокси (см. README: Envoy).
- Бэкенд (budgetd) реализует доменную логику и хранит данные в PostgreSQL.
- Протокол и модели описаны в `proto/`, код генерируется в `gen/` и `web/proto/`.
- Миграции базы — в `migrations/`.

## Из чего состоит
- `cmd/budgetd/` — точка входа бэкенда.
- `internal/`
  - `domain/` — доменные сущности.
  - `usecase/` — прикладные сервисы.
  - `adapter/` — инфраструктурные адаптеры (Postgres, Redis, gRPC, auth).
  - `pkg/` — общие утилиты.
- `web/` — фронтенд на Next.js (App Router), UI, i18n, gRPC‑клиенты.
- `proto/` — protobuf схемы API.
- `gen/` — сгенерированный Go‑код из protobuf.
- `migrations/` — миграции БД.
- `docker-compose*.yml`, `scripts/`, `Makefile` — локальный запуск/CI/деплой.

## Быстрый старт (по README)
- `make up` — поднять окружение.
- `make logs` — логи.
- Сервисы: фронтенд `http://localhost:3030`, gRPC `localhost:8080`.

## Полезные файлы
- `README.md`, `README_EN.md` — основная документация.
- `env.example` — переменные окружения.
- `docs/GOOGLE_AUTH_SETUP.md` — пошаговая настройка Google auth (GCP, `.env`, k3s, секреты).
- `IMPLEMENTATION_PLAN.md`, `FRONTEND_IMPLEMENTATION_PLAN.md` — планы/идеи.
- `web/README.md` — детали по фронтенду.
- `doc/agent-guides/create-transaction-api-flow.md` — подробный флоу создания транзакции через API.

## Контекст для быстрого входа
- Основной домен: транзакции, категории, отчеты, организации (tenant).
- Аутентификация: JWT/refresh‑токены, Argon2id + вход/регистрация через Google ID token (`AuthService.GoogleAuth`).
- Интеграции: gRPC, PostgreSQL, мониторинг (Prometheus/Grafana), OpenTelemetry.

## OAuth Google (web)
- Web логин/регистрация используют Google Identity Services и передают `id_token` в `AuthService.GoogleAuth`.
- Backend валидирует токен через Google `tokeninfo` endpoint и `GOOGLE_CLIENT_ID` (audience check).
- Пользователь ищется по email; если не найден, создается аккаунт + дефолтный tenant.
- Поле tenant/company в регистрации опционально: пустое значение приводит к стандартному имени tenant (`My Budget` в репозитории).

### Обязательные env для Google web auth
- Backend: `GOOGLE_CLIENT_ID`
- Frontend: `NEXT_PUBLIC_GOOGLE_CLIENT_ID`

### Kubernetes (budget app)
- `devops-time-host/apps/budget/base/configmap.yaml` содержит:
  - `GOOGLE_CLIENT_ID`
  - `NEXT_PUBLIC_GOOGLE_CLIENT_ID`
- `devops-time-host/apps/budget/base/app-deployment.yaml` прокидывает `GOOGLE_CLIENT_ID` в `budget-app`.
- `devops-time-host/apps/budget/base/web-deployment.yaml` прокидывает `NEXT_PUBLIC_GOOGLE_CLIENT_ID` в `budget-web`.

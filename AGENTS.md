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
- `IMPLEMENTATION_PLAN.md`, `FRONTEND_IMPLEMENTATION_PLAN.md` — планы/идеи.
- `web/README.md` — детали по фронтенду.
- `doc/agent-guides/create-transaction-api-flow.md` — подробный флоу создания транзакции через API.

## Контекст для быстрого входа
- Основной домен: транзакции, категории, отчеты, организации (tenant).
- Аутентификация: JWT/refresh‑токены, Argon2id.
- Интеграции: gRPC, PostgreSQL, мониторинг (Prometheus/Grafana), OpenTelemetry.

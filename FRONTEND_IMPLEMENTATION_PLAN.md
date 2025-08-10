## Пошаговый план реализации Frontend (Next.js + TypeScript + gRPC-Web)

Ниже — детальный план реализации фронтенда в монорепозитории, по аналогии с `IMPLEMENTATION_PLAN.md`. Фронтенд использует gRPC-Web (через Envoy 8081) и TS-клиент, сгенерированный из тех же protobuf‑схем.

### Текущий статус

- 🟡 Подготовка окружения и scaffolding
- 🟡 Генерация TS‑клиентов (buf + connect‑web)
- 🟡 Архитектура приложения, каркас навигации и авторизации
- 🟡 Базовые страницы: Login/Register, Tenant Switcher, Profile
- 🟡 Категории: List/CRUD, i18n‑переводы
- 🟡 Транзакции: список с фильтрами/пагинацией, создание/редактирование/удаление
- 🟡 Отчеты: месячная сводка
- 🟡 Fx: просмотр/изменение курсов
- 🟡 Тесты (unit/component) + E2E, CI

Цель: все пункты выше перевести в ✅ «сделано». Импорт CSV будет отдельным этапом и не блокирует фронтенд MVP.

---

### 0. Технологический стек

- Next.js 14 (App Router) + TypeScript
- Клиент gRPC‑Web: `@connectrpc/connect-web` + сгенерированный TS код из proto
- UI/UX: Tailwind CSS + Radix UI + Headless UI (минимум зависимостей)
- Состояние/запросы: TanStack Query (React Query)
- Формы: React Hook Form + Zod (валидация)
- i18n: `next-intl` (или `@lingui/core`) — ru/en
- Тесты: Vitest + React Testing Library + Playwright (E2E)
- Линтинг/форматирование: ESLint + Prettier + Stylelint (опционально для CSS)
- Пакетный менеджер: pnpm (или npm/yarn, но рекомендуем pnpm)

---

### 1. Окружение, scaffold и структура проекта

- Добавить каталог `web/` в корень монорепозитория.
- `web/package.json`: скрипты `dev`, `build`, `start`, `lint`, `test`, `e2e`.
- Node.js 20.x, pnpm 9.x (или npm) — зафиксировать в `.nvmrc`/Volta (опционально).
- Структура `web/` (целевой вид):

```
web/
  app/                 # Next.js App Router
    (auth)/login/page.tsx
    (auth)/register/page.tsx
    (dashboard)/page.tsx
    (transactions)/page.tsx
    (categories)/page.tsx
    (reports)/monthly/page.tsx
    (fx)/page.tsx
    (settings)/profile/page.tsx
    layout.tsx
    globals.css
  components/          # UI компоненты
  lib/                 # утилиты (auth, tenants, api, formatters)
  proto/               # сгенерированные TS клиенты (buf generate)
  hooks/
  styles/
  tests/               # unit/component тесты
  e2e/                 # e2e тесты Playwright
  env.d.ts
  .eslintrc.cjs
  .prettierrc.cjs
  tailwind.config.ts
  postcss.config.cjs
```

Проверка:

```bash
cd web && pnpm install && pnpm dev
```

---

### 2. Генерация TS‑клиентов из protobuf

- В `proto/buf.gen.yaml` добавить TS‑генерацию (если ещё нет) для `connect-web`:

```yaml
plugins:
  - plugin: buf.build/connectrpc/es
    out: ../web/proto
    opt:
      - target=web
      - import_extension=none
```

- Команда генерации:

```bash
cd proto
buf lint && buf generate
```

- В `web/lib/api/transport.ts` настроить Connect transport к Envoy:

```ts
import { createConnectTransport } from "@connectrpc/connect-web";

export const transport = createConnectTransport({
  baseUrl: process.env.NEXT_PUBLIC_GRPC_BASE_URL ?? "http://localhost:8081",
});
```

---

### 3. Авторизация и контекст арендатора

- Auth flow (совместимо с бэком):
  - Login: отправка email/password → получение access/refresh токенов.
  - Хранение токенов: в dev можно localStorage; в prod — предпочтительнее httpOnly cookie через BFF/прокси (в этом проекте — оставить в localStorage, добавить TODO по BFF).
  - Refresh: по 401/Unauthenticated — попытка refresh; при ошибке — logout.
  - Tenant: активный `tenant_id` — хранить в Zustand/Query cache + localStorage; отправлять в metadata заголовок `x-tenant-id` на каждый запрос.

- Реализовать `authClient` (обёртки над proto‑клиентом) и перехватчики:
  - Добавление `authorization: Bearer <access>` и `x-tenant-id` в metadata.
  - Глобальная обработка ошибок gRPC (codes → UX): `Unauthenticated`, `PermissionDenied`, `InvalidArgument`, `NotFound` → тосты/notifications.

---

### 4. Навигация и layout

- Основной layout: хедер (Switcher арендатора, Current user), sidebar (разделы: Dashboard, Transactions, Categories, Reports, Fx, Settings).
- Роутинг (App Router): публичные маршруты `(auth)`, защищённые — всё остальное (guard по наличию access токена).
- i18n переключатель (ru/en), сохранение в настройках пользователя (при наличии API `UpdateProfile`).

---

### 5. Страницы и фичи

1) Auth
- `/auth/login`: форма email/password, валидация (Zod), обработка ошибок бэка.
- `/auth/register`: email/password/name/locale/tenant_name → автоматический login.

2) Dashboard
- `/` (dashboard): `ReportService.GetMonthlySummary` (за текущий месяц), перечисление top‑категорий, сводные суммы (доход/расход).

3) Categories
- `/categories`: список по виду (income/expense) с i18n‑переводами; CRUD (диалоги создания/редактирования), валидация уникальности `code` (обработка ошибок).

4) Transactions
- `/transactions`:
  - Список с фильтрами: диапазон дат, категории, тип (income/expense), сумма от/до, валюта, поиск, пагинация.
  - Создание/редактирование: сумма (`Money`), валюта, категория, дата/время, комментарий.
  - Удаление: подтверждение, тосты.

5) Reports
- `/reports/monthly`: выбор месяца/года, целевая валюта, локаль; таблица/диаграмма по категориям.

6) Fx
- `/fx`: просмотр курсов за день; upsert курса; batch загрузка (минимум UI).

7) Settings
- `/settings/profile`: имя/locale, смена пароля.
- `/settings/tenants`: список моих тенантов, создание нового (name/slug/base currency).

---

### 6. Состояние, кэш и форматирование

- TanStack Query: queryKeys по сервисам (`["categories", {kind, tenant}]`, `["tx", filters]`, и т.п.).
- Инвалидация кэша после мутаций.
- Отображение сумм: компонент `Money` (формат minor units → human), локаль/валюта; дата — `date-fns`.

---

### 7. Тестирование

- Unit/Component:
  - Vitest + React Testing Library: формы (валидации/ошибки), компоненты отображения, state hooks.
  - Моки gRPC‑клиентов (интерфейсы обёрток) для изоляции UI.
- E2E:
  - Playwright: сценарии Login→Dashboard, Categories CRUD, Transactions Create/List, Reports view.

Команды:

```bash
pnpm test          # unit/component
pnpm test:e2e      # playwright
```

---

### 8. CI/CD (frontend)

- В существующий GitHub Actions добавить job `web`:
  - `actions/setup-node@v4` (Node 20.x), кеш pnpm.
  - `pnpm i --frozen-lockfile`.
  - `pnpm lint && pnpm test && pnpm build`.
  - (опционально) `pnpm test:e2e` в docker (devserver + playwright).

---

### 9. Настройки окружения

- `web/.env.local`:

```
NEXT_PUBLIC_GRPC_BASE_URL=http://localhost:8081
NEXT_PUBLIC_DEFAULT_LOCALE=ru
```

- Для dev окружения бэкенд: `GRPC_ADDR=:8080`, Envoy `8081`, база `DATABASE_URL=...`.

---

### 10. Критерии готовности фронтенда (MVP)

- Auth/tenants: вход/регистрация, выбор активного тенанта, безопасное хранение токенов для dev.
- Categories: CRUD с локализацией, обработка ошибок.
- Transactions: список с фильтрами и CRUD, пагинация.
- Reports: корректная месячная сводка (совпадает с бэкендом на тестовых данных).
- Fx: базовый экран для просмотра/изменения курса.
- Тесты: базовые unit/component + smoke e2e, CI зелёный.

---

### 11. План работ по этапам

1) Scaffolding + infra (Next.js, Tailwind, ESLint/Prettier, Query, RHF, Zod) — ✅
2) Генерация TS‑клиентов (buf) и transport — ✅
3) Auth + TenantSwitcher + Layout — 🟡 (сделано: Login, Protected, хранение токенов; осталось: Tenant switcher)
4) Categories (List/CRUD + i18n) — 🟡 (сделано: List + Create/Delete)
5) Transactions (List/Filters/CRUD) — 🟡 (сделано: минимальный List + Create)
6) Reports (Monthly) — 🟡 (сделано: минимальный вызов и вывод JSON)
7) Fx (Get/Upsert/Batch) — 🟡
8) Settings (Profile/Password) — 🟡
9) Тесты unit/component + smoke e2e — 🟡 (добавлен smoke Vitest)
10) CI job `web` — ✅
11) Регистрация — ✅ (страница Register, автологин, запись токенов)

По мере выполнения отмечать статусы и поддерживать «зелёные» проверки.



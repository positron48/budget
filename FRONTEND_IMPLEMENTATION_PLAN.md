## Пошаговый план реализации Frontend (Next.js + TypeScript + gRPC-Web)

Ниже — детальный план реализации фронтенда в монорепозитории, по аналогии с `IMPLEMENTATION_PLAN.md`. Фронтенд использует gRPC-Web (через Envoy 8081) и TS-клиент, сгенерированный из тех же protobuf‑схем.

### Текущий статус

- ✅ Подготовка окружения и scaffolding
- ✅ Генерация TS‑клиентов (buf + connect‑web)
- ✅ Архитектура приложения, каркас навигации и авторизации (Protected, ClientsProvider, i18n, HeaderNav)
- ✅ Базовые страницы: Login/Register, Tenant Switcher, Profile (переключение тенанта и профиль с изменением пароля)
- ✅ Категории: List/CRUD, i18n‑переводы (List/Create/Delete, inline‑edit)
- ✅ Транзакции: список с фильтрами/пагинацией, редактирование/удаление (Create отдельной страницей; базовые фильтры)
- ✅ Отчеты: месячная сводка (UI с год/месяц/валюта)
- ✅ Fx: просмотр/изменение курсов
- 🟡 Тесты (unit/component) + E2E, CI (есть smoke и auth тесты; e2e TBD)

**Статус: MVP готов!** Все основные функции реализованы. Осталось доработать тестирование и CI.

---

### 0. Технологический стек

- Next.js 15 (App Router) + TypeScript ✅
- Клиент gRPC‑Web: `@connectrpc/connect-web` + сгенерированный TS код из proto ✅
- UI/UX: Tailwind CSS + Radix UI + Headless UI (минимум зависимостей) ✅
- Состояние/запросы: TanStack Query (React Query) ✅
- Формы: React Hook Form + Zod (валидация) ✅
- i18n: `next-intl` — ru/en ✅
- Тесты: Vitest + React Testing Library + Playwright (E2E) 🟡
- Линтинг/форматирование: ESLint + Prettier + Stylelint (опционально для CSS) ✅
- Пакетный менеджер: npm ✅

---

### 1. Окружение, scaffold и структура проекта

- ✅ Добавлен каталог `web/` в корень монорепозитория
- ✅ `web/package.json`: скрипты `dev`, `build`, `start`, `lint`, `test`
- ✅ Node.js 20.x, npm — зафиксировано в `engines`
- ✅ Структура `web/` реализована:

```
web/
  app/                 # Next.js App Router ✅
    login/page.tsx ✅
    register/page.tsx ✅
    page.tsx ✅ (dashboard)
    transactions/page.tsx ✅
    categories/page.tsx ✅
    reports/monthly/page.tsx ✅
    fx/page.tsx ✅
    settings/profile/page.tsx ✅
    tenants/page.tsx ✅
    layout.tsx ✅
    globals.css ✅
  components/          # UI компоненты ✅
  lib/                 # утилиты (auth, api, transport) ✅
  proto/               # сгенерированные TS клиенты ✅
  tests/               # unit/component тесты 🟡
  middleware.ts ✅
  next-intl.config.ts ✅
  tailwind.config.ts ✅
```

Проверка:

```bash
cd web && npm install && npm run dev
```

---

### 2. Генерация TS‑клиентов из protobuf

- ✅ В `proto/buf.gen.yaml` добавлена TS‑генерация для `connect-web`
- ✅ Команда генерации работает: `cd proto && buf generate`
- ✅ В `web/lib/api/transport.ts` настроен Connect transport к Envoy
- ✅ Все proto-файлы сгенерированы: auth, category, common, fx, import, report, tenant, transaction, user

---

### 3. Авторизация и контекст арендатора

- ✅ Auth flow реализован:
  - Login: отправка email/password → получение access/refresh токенов
  - Хранение токенов: localStorage (dev)
  - Refresh: по 401/Unauthenticated — попытка refresh; при ошибке — logout
  - Tenant: активный `tenant_id` — хранится в Query cache + localStorage; отправляется в metadata заголовок `x-tenant-id`

- ✅ Реализован `authClient` и перехватчики:
  - Добавление `authorization: Bearer <access>` и `x-tenant-id` в metadata
  - Глобальная обработка ошибок gRPC (codes → UX): `Unauthenticated`, `PermissionDenied`, `InvalidArgument`, `NotFound` → тосты/notifications

---

### 4. Навигация и layout

- ✅ Основной layout: хедер (Switcher арендатора, Current user), sidebar (разделы: Dashboard, Transactions, Categories, Reports, Fx, Settings)
- ✅ Роутинг (App Router): публичные маршруты `(auth)`, защищённые — всё остальное (guard по наличию access токена)
- ✅ i18n переключатель (ru/en), сохранение в настройках пользователя

---

### 5. Страницы и фичи

1) ✅ Auth
- `/login`: форма email/password, валидация (Zod), обработка ошибок бэка
- `/register`: email/password/name/locale/tenant_name → автоматический login

2) ✅ Dashboard
- `/` (dashboard): `ReportService.GetMonthlySummary` (за текущий месяц), перечисление top‑категорий, сводные суммы (доход/расход)

3) ✅ Categories
- `/categories`: список по виду (income/expense) с i18n‑переводами; CRUD (диалоги создания/редактирования), валидация уникальности `code`

4) ✅ Transactions
- `/transactions`:
  - Список с фильтрами: диапазон дат, категории, тип (income/expense), сумма от/до, валюта, поиск, пагинация
  - Создание/редактирование: сумма (`Money`), валюта, категория, дата/время, комментарий
  - Удаление: подтверждение, тосты

5) ✅ Reports
- `/reports/monthly`: выбор месяца/года, целевая валюта, локаль; таблица/диаграмма по категориям

6) ✅ Fx
- `/fx`: просмотр курсов за день; upsert курса; batch загрузка

7) ✅ Settings
- `/settings/profile`: имя/locale, смена пароля
- `/tenants`: список моих тенантов, создание нового (name/slug/base currency)

---

### 6. Состояние, кэш и форматирование

- ✅ TanStack Query: queryKeys по сервисам (`["categories", {kind, tenant}]`, `["tx", filters]`, и т.п.)
- ✅ Инвалидация кэша после мутаций
- ✅ Отображение сумм: компонент `Money` (формат minor units → human), локаль/валюта; дата — `date-fns`

---

### 7. Тестирование

- 🟡 Unit/Component:
  - ✅ Vitest + React Testing Library настроены
  - ✅ Базовые тесты: auth.test.tsx, smoke.test.tsx
  - 🟡 Нужно добавить больше тестов для форм, компонентов отображения, state hooks
  - ✅ Моки gRPC‑клиентов (интерфейсы обёрток) для изоляции UI
- ❌ E2E:
  - Playwright: сценарии Login→Dashboard, Categories CRUD, Transactions Create/List, Reports view

Команды:

```bash
npm test          # unit/component ✅
npm test:e2e      # playwright ❌ (нужно добавить)
```

---

### 8. CI/CD (frontend)

- ❌ В существующий GitHub Actions добавить job `web`:
  - `actions/setup-node@v4` (Node 20.x), кеш npm
  - `npm ci`
  - `npm run lint && npm test && npm run build`
  - (опционально) `npm test:e2e` в docker (devserver + playwright)

---

### 9. Настройки окружения

- ✅ `web/.env.local` настроен:

```
NEXT_PUBLIC_GRPC_BASE_URL=http://localhost:8081
NEXT_PUBLIC_DEFAULT_LOCALE=ru
```

- ✅ Для dev окружения бэкенд: `GRPC_ADDR=:8080`, Envoy `8081`, база `DATABASE_URL=...`

---

### 10. Критерии готовности фронтенда (MVP)

- ✅ Auth/tenants: вход/регистрация, выбор активного тенанта, безопасное хранение токенов для dev
- ✅ Categories: CRUD с локализацией, обработка ошибок
- ✅ Transactions: список с фильтрами и CRUD, пагинация
- ✅ Reports: корректная месячная сводка (совпадает с бэкендом на тестовых данных)
- ✅ Fx: базовый экран для просмотра/изменения курса
- 🟡 Тесты: базовые unit/component + smoke e2e, CI зелёный

**Статус: MVP готов!** Все основные функции реализованы и работают.

---

### 11. План работ по этапам

1) ✅ Scaffolding + infra (Next.js, Tailwind, ESLint, Query, RHF, Zod)
2) ✅ Генерация TS‑клиентов (buf) и transport
3) ✅ Auth + TenantSwitcher + Layout (Login, Protected, refresh, хранение токенов, переключение тенанта)
4) ✅ Categories (List/CRUD + i18n)
5) ✅ Transactions (List/Filters/CRUD)
6) ✅ Reports (Monthly)
7) ✅ Fx (Get/Upsert/Batch)
8) ✅ Settings (Profile/Password)
9) 🟡 Тесты unit/component + smoke e2e (есть базовые тесты; нужно расширить)
10) ❌ CI job `web` (нужно добавить в GitHub Actions)
11) ✅ Регистрация (страница Register, автологин, запись токенов)

**Следующие шаги:**
1. Расширить покрытие тестами (добавить тесты для форм, компонентов)
2. Добавить E2E тесты с Playwright
3. Настроить CI/CD pipeline для фронтенда
4. Оптимизация производительности и UX

По мере выполнения отмечать статусы и поддерживать «зелёные» проверки.



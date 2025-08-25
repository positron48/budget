# 💰 Budget - Многопользовательский учет доходов и расходов

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org/)
[![Next.js](https://img.shields.io/badge/Next.js-14+-000000?style=for-the-badge&logo=next.js&logoColor=white)](https://nextjs.org/)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.0+-3178C6?style=for-the-badge&logo=typescript&logoColor=white)](https://www.typescriptlang.org/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-15+-336791?style=for-the-badge&logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Docker](https://img.shields.io/badge/Docker-✓-2496ED?style=for-the-badge&logo=docker&logoColor=white)](https://www.docker.com/)

[![CI](https://github.com/positron48/budget/workflows/CI/badge.svg)](https://github.com/positron48/budget/actions/workflows/ci.yml)
[![Security Audit](https://github.com/positron48/budget/workflows/Security%20Audit/badge.svg)](https://github.com/positron48/budget/actions/workflows/security.yml)
[![Generate Stubs](https://github.com/positron48/budget/workflows/Generate%20Protobuf%20Stubs/badge.svg)](https://github.com/positron48/budget/actions/workflows/generate-stubs.yml)
[![License](https://img.shields.io/badge/License-MIT-yellow?style=for-the-badge)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-Welcome-brightgreen?style=for-the-badge)](http://makeapullrequest.com)
[![Go Report Card](https://goreportcard.com/badge/github.com/positron48/budget)](https://goreportcard.com/report/github.com/positron48/budget)
[![Codecov](https://img.shields.io/badge/coverage-80%25-brightgreen?style=for-the-badge)](https://codecov.io/gh/positron48/budget)
[![Dependabot](https://img.shields.io/badge/dependabot-enabled-025e8c?style=for-the-badge&logo=dependabot)](https://dependabot.com/)
[![GitHub stars](https://img.shields.io/github/stars/positron48/budget?style=for-the-badge)](https://github.com/positron48/budget/stargazers)
[![GitHub forks](https://img.shields.io/github/forks/positron48/budget?style=for-the-badge)](https://github.com/positron48/budget/network)
[![GitHub issues](https://img.shields.io/github/issues/positron48/budget?style=for-the-badge)](https://github.com/positron48/budget/issues)
[![GitHub pull requests](https://img.shields.io/github/issues-pr/positron48/budget?style=for-the-badge)](https://github.com/positron48/budget/pulls)

[English](README_EN.md) | [Русский](README.md)

> Современное веб-приложение для учета личных финансов с поддержкой многопользовательности, импорта/экспорта данных и красивым интерфейсом.

## 🚀 Быстрый старт

<div align="center">

[![Deploy with Docker](https://img.shields.io/badge/Deploy%20with-Docker-2496ED?style=for-the-badge&logo=docker&logoColor=white)](https://github.com/positron48/budget#%EF%B8%8F-установка-и-запуск)
[![Run with Make](https://img.shields.io/badge/Run%20with-Make-FF6B6B?style=for-the-badge&logo=gnu&logoColor=white)](https://github.com/positron48/budget#%EF%B8%8F-установка-и-запуск)
[![View Demo](https://img.shields.io/badge/View-Demo-00C851?style=for-the-badge&logo=github&logoColor=white)](https://github.com/positron48/budget#доступ-к-сервисам)

</div>

### Предварительные требования
- Docker и Docker Compose
- Git

### Установка и запуск
```bash
# Клонирование репозитория
git clone <repository>
cd budget

# Запуск всего окружения
make up

# Проверка статуса
make logs
```

### Доступ к сервисам
- 🌐 **Frontend**: http://localhost:3030
- 🔧 **Backend gRPC**: localhost:8080
- 📊 **Grafana**: http://localhost:3002
- 📈 **Prometheus**: http://localhost:9090

### Первые шаги
1. Откройте http://localhost:3030
2. Зарегистрируйтесь с email/паролем
3. Создайте первую организацию
4. Начните добавлять категории и транзакции

## ✨ Основные возможности

<div align="center">

[![Transactions](https://img.shields.io/badge/💳-Transactions-00BCD4?style=for-the-badge)](https://github.com/positron48/budget#-управление-транзакциями)
[![Analytics](https://img.shields.io/badge/📊-Analytics-4CAF50?style=for-the-badge)](https://github.com/positron48/budget#-аналитика-и-отчеты)
[![Import/Export](https://img.shields.io/badge/🔄-Import/Export-FF9800?style=for-the-badge)](https://github.com/positron48/budget#-импортэкспорт-данных)
[![Multi-User](https://img.shields.io/badge/👥-Multi--User-9C27B0?style=for-the-badge)](https://github.com/positron48/budget#-многопользовательность)
[![i18n](https://img.shields.io/badge/🌍-i18n-2196F3?style=for-the-badge)](https://github.com/positron48/budget#-интернационализация)

</div>

### 💳 Управление транзакциями
- ✅ CRUD операции с транзакциями (доходы/расходы)
- ✅ Категоризация с поддержкой i18n
- ✅ Фильтрация и поиск с быстрыми фильтрами
- ✅ Пагинация и сортировка
- ✅ Экспорт в CSV с фильтрами
- ✅ Быстрые фильтры (текущий месяц, прошлый месяц, год, 30 дней)

### 📊 Аналитика и отчеты
- ✅ Месячные отчеты по категориям
- ✅ Визуализация данных с графиками
- ✅ Сравнение периодов
- ✅ Статистика доходов и расходов

### 🔄 Импорт/Экспорт данных
- ✅ Импорт CSV файлов с настройкой маппинга
- ✅ Автоматическое определение кодировки
- ✅ Предварительный просмотр данных
- ✅ Экспорт с учетом всех фильтров

### 👥 Многопользовательность
- ✅ Multi-tenant архитектура
- ✅ Роли: Owner, Admin, Member
- ✅ Управление организациями
- ✅ Изоляция данных между аккаунтами

### 🌍 Интернационализация
- ✅ Поддержка русского и английского языков
- ✅ Локализованные категории
- ✅ Автоматическое переключение языков

## 🏗️ Архитектура

```mermaid
flowchart TB
  subgraph Client
    Web["🌐 Web App (Next.js + TS)"]
    TG["📱 Telegram Bot (Go) 📋"]
  end

  subgraph Edge
    Envoy["🔄 Envoy Proxy<br/>gRPC-Web → gRPC"]
  end

  subgraph Core
    Budgetd["⚙️ budgetd (Go)<br/>Hexagonal Architecture<br/>– gRPC API<br/>– Domain Services<br/>– Repository Pattern"]
  end

  subgraph Infra
    PG["🗄️ PostgreSQL 15+"]
    MQ["📨 NATS/RabbitMQ 📋"]
    S3["☁️ S3 Storage 📋"]
    OTEL["📊 OpenTelemetry"]
  end

  Web -->|gRPC-Web| Envoy -->|gRPC| Budgetd
  TG -->|gRPC| Budgetd
  Budgetd <-->|SQL| PG
  Budgetd -->|events| MQ
  Budgetd -->|files| S3
  Budgetd --> OTEL
```

## 🛠️ Технологический стек

<div align="center">

[![Go](https://img.shields.io/badge/Backend-Go%201.23+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org/)
[![gRPC](https://img.shields.io/badge/API-gRPC-4285F4?style=for-the-badge&logo=google&logoColor=white)](https://grpc.io/)
[![PostgreSQL](https://img.shields.io/badge/Database-PostgreSQL%2015+-336791?style=for-the-badge&logo=postgresql&logoColor=white)](https://www.postgresql.org/)

[![Next.js](https://img.shields.io/badge/Frontend-Next.js%2014-000000?style=for-the-badge&logo=next.js&logoColor=white)](https://nextjs.org/)
[![TypeScript](https://img.shields.io/badge/Language-TypeScript-3178C6?style=for-the-badge&logo=typescript&logoColor=white)](https://www.typescriptlang.org/)
[![Tailwind](https://img.shields.io/badge/Styling-Tailwind%20CSS-06B6D4?style=for-the-badge&logo=tailwind-css&logoColor=white)](https://tailwindcss.com/)

[![Docker](https://img.shields.io/badge/DevOps-Docker-2496ED?style=for-the-badge&logo=docker&logoColor=white)](https://www.docker.com/)
[![Prometheus](https://img.shields.io/badge/Monitoring-Prometheus-E6522C?style=for-the-badge&logo=prometheus&logoColor=white)](https://prometheus.io/)
[![GitHub Actions](https://img.shields.io/badge/CI/CD-GitHub%20Actions-2088FF?style=for-the-badge&logo=github-actions&logoColor=white)](https://github.com/features/actions)

</div>

### Backend
- **Go 1.23+** - основной язык сервера
- **gRPC** - API протокол с protobuf
- **PostgreSQL 15+** - основная база данных
- **Argon2id** - хеширование паролей
- **JWT** - аутентификация с refresh токенами

### Frontend
- **Next.js 14** - React фреймворк
- **TypeScript** - типизированный JavaScript
- **Tailwind CSS** - утилитарный CSS фреймворк
- **TanStack Query** - управление состоянием
- **Connect-Web** - gRPC клиент для браузера

### DevOps
- **Docker** - контейнеризация
- **Prometheus + Grafana** - мониторинг
- **OpenTelemetry** - трейсинг и метрики
- **GitHub Actions** - CI/CD

## 📊 Статус реализации

| Компонент | Статус | Описание |
|-----------|--------|----------|
| 🔐 **Аутентификация** | ✅ Готово | Регистрация, вход, JWT токены |
| 👤 **Пользователи** | ✅ Готово | Профили, смена пароля |
| 🏢 **Организации** | ✅ Готово | Multi-tenant, роли |
| 📂 **Категории** | ✅ Готово | CRUD с i18n |
| 💰 **Транзакции** | ✅ Готово | CRUD, фильтры, пагинация |
| 📈 **Отчеты** | ✅ Готово | Месячная аналитика |
| 💱 **Курсы валют** | ✅ Готово | Управление FX |
| 📥 **Импорт** | ✅ Готово | CSV с маппингом |
| 📤 **Экспорт** | ✅ Готово | CSV с фильтрами |
| 🌐 **Frontend** | ✅ Готово | Современный UI/UX |
| 📊 **Мониторинг** | ✅ Готово | Prometheus + Grafana |
| 🧪 **Тесты** | ✅ Готово | Unit + Integration |

## 🧪 Тестирование

```bash
# Backend тесты
make test

# Frontend тесты
cd web && npm test

# Все тесты
make check
```

**Результаты тестов:**
- ✅ **13 тестов** - все проходят
- ✅ **Backend** - unit и integration тесты
- ✅ **Frontend** - компонентные тесты
- ✅ **API** - gRPC интеграционные тесты

### Frontend
- **Next.js 14** - React фреймворк
- **TypeScript** - типизированный JavaScript
- **Tailwind CSS** - утилитарный CSS фреймворк
- **TanStack Query** - управление состоянием
- **Connect-Web** - gRPC клиент для браузера

### DevOps
- **Docker** - контейнеризация
- **Prometheus + Grafana** - мониторинг
- **OpenTelemetry** - трейсинг и метрики
- **GitHub Actions** - CI/CD

## 📊 Статус реализации

| Компонент | Статус | Описание |
|-----------|--------|----------|
| 🔐 **Аутентификация** | ✅ Готово | Регистрация, вход, JWT токены |
| 👤 **Пользователи** | ✅ Готово | Профили, смена пароля |
| 🏢 **Организации** | ✅ Готово | Multi-tenant, роли |
| 📂 **Категории** | ✅ Готово | CRUD с i18n |
| 💰 **Транзакции** | ✅ Готово | CRUD, фильтры, пагинация |
| 📈 **Отчеты** | ✅ Готово | Месячная аналитика |
| 💱 **Курсы валют** | ✅ Готово | Управление FX |
| 📥 **Импорт** | ✅ Готово | CSV с маппингом |
| 📤 **Экспорт** | ✅ Готово | CSV с фильтрами |
| 🌐 **Frontend** | ✅ Готово | Современный UI/UX |
| 📊 **Мониторинг** | ✅ Готово | Prometheus + Grafana |
| 🧪 **Тесты** | ✅ Готово | Unit + Integration |



## 🚀 Развертывание

### Локальная разработка
```bash
# Запуск всего окружения
make up

# Остановка
make down

# Проверки
make check

# Генерация protobuf
make proto

# Миграции БД
make migrate-up
```

### Продакшн
```bash
# Сборка и запуск
docker-compose -f docker-compose.yml up -d

# Мониторинг
docker-compose -f docker-compose.yml logs -f
```

## 📁 Структура проекта

```
budget/
├── 📁 cmd/budgetd/              # Backend entry point
├── 📁 internal/                 # Backend business logic
│   ├── 📁 domain/              # Domain entities
│   ├── 📁 usecase/             # Application services
│   ├── 📁 adapter/             # Infrastructure adapters
│   └── 📁 pkg/                 # Shared utilities
├── 📁 web/                     # Frontend (Next.js)
│   ├── 📁 app/                 # Next.js App Router
│   ├── 📁 components/          # React components
│   ├── 📁 lib/                 # Utilities and API clients
│   └── 📁 i18n/                # Internationalization
├── 📁 proto/                   # gRPC schemas
├── 📁 migrations/              # Database migrations
├── 📁 deploy/                  # Docker and monitoring
└── 📁 docs/                    # Documentation
```

## 🔧 Основные команды

| Команда | Описание |
|---------|----------|
| `make up` | Запуск всего окружения |
| `make down` | Остановка всех сервисов |
| `make check` | Проверки backend + frontend |
| `make proto` | Генерация protobuf кода |
| `make migrate-up` | Применение миграций БД |
| `make test` | Запуск тестов |
| `make logs` | Просмотр логов |

## 🌍 Интернационализация

Приложение полностью поддерживает **русский** и **английский** языки:

- ✅ **Backend**: i18n для категорий и сообщений
- ✅ **Frontend**: полная локализация интерфейса
- ✅ **Автоматическое переключение** языков
- ✅ **Сохранение** выбранного языка в профиле

## 🔐 Безопасность

- ✅ **Argon2id** хеширование паролей
- ✅ **JWT** с access/refresh токенами
- ✅ **Multi-tenant** изоляция данных
- ✅ **Роли** в аккаунтах (Owner/Admin/Member)
- ✅ **Валидация** на всех уровнях
- ✅ **Автоматическое обновление** токенов

## 📈 Мониторинг

- ✅ **Prometheus** метрики
- ✅ **OpenTelemetry** трейсинг
- ✅ **Grafana** дашборды
- ✅ **Структурированное** логирование

## 🎨 UI/UX

- ✅ **Современный дизайн** с Tailwind CSS
- ✅ **Адаптивная верстка** для всех устройств
- ✅ **Плавные анимации** и переходы
- ✅ **Интуитивная навигация**
- ✅ **Обратная связь** для всех действий

## 📋 Roadmap

### 🚧 В разработке
- 📱 **Telegram бот** для быстрого добавления транзакций
- 🏦 **Интеграции с банками** для автоматического импорта
- 📊 **Планирование бюджета** и финансовые цели

### 🔮 Планируется
- 📱 **Мобильное приложение** (React Native)
- 🧪 **E2E тесты** с Playwright
- ⚡ **Performance оптимизации**
- 📱 **PWA функциональность**
- 🔄 **Оффлайн режим**
- 🔔 **Push уведомления**

## 🤝 Вклад в проект

Мы приветствуем вклад в развитие проекта! Пожалуйста, ознакомьтесь с нашими [правилами контрибьюции](CONTRIBUTING.md).

### Как помочь:
1. 🐛 **Сообщить о баге** - создайте issue
2. 💡 **Предложить идею** - создайте feature request
3. 🔧 **Исправить баг** - создайте pull request
4. 📚 **Улучшить документацию** - отредактируйте README

## 📄 Лицензия

Этот проект лицензирован под MIT License - см. файл [LICENSE](LICENSE) для деталей.

## 🙏 Благодарности

- [Go](https://golang.org/) - за отличный язык программирования
- [Next.js](https://nextjs.org/) - за современный React фреймворк
- [Tailwind CSS](https://tailwindcss.com/) - за утилитарный CSS
- [gRPC](https://grpc.io/) - за эффективный API протокол

---

<div align="center">

[![Star on GitHub](https://img.shields.io/github/stars/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/stargazers)
[![Fork on GitHub](https://img.shields.io/github/forks/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/network)
[![Watch on GitHub](https://img.shields.io/github/watchers/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/watchers)

[![Issues](https://img.shields.io/github/issues/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/issues)
[![Pull Requests](https://img.shields.io/github/issues-pr/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/pulls)
[![Discussions](https://img.shields.io/github/discussions/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/discussions)

[![Contributors](https://img.shields.io/github/contributors/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/graphs/contributors)
[![Last Commit](https://img.shields.io/github/last-commit/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/commits)
[![Release](https://img.shields.io/github/release/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/releases)

</div>

**⭐ Если проект вам понравился, поставьте звездочку!**

**Статус проекта**: ✅ **MVP готов к использованию**

Все основные функции реализованы и протестированы. Приложение готово для использования в продакшене с базовым функционалом учета личных финансов.



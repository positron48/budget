# Budget - Многопользовательский учет доходов и расходов

**Технологии**

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org/) [![Next.js](https://img.shields.io/badge/Next.js-14+-000000?style=for-the-badge&logo=next.js&logoColor=white)](https://nextjs.org/) [![TypeScript](https://img.shields.io/badge/TypeScript-5.0+-3178C6?style=for-the-badge&logo=typescript&logoColor=white)](https://www.typescriptlang.org/) [![PostgreSQL](https://img.shields.io/badge/PostgreSQL-15+-336791?style=for-the-badge&logo=postgresql&logoColor=white)](https://www.postgresql.org/) [![Docker](https://img.shields.io/badge/Docker-✓-2496ED?style=for-the-badge&logo=docker&logoColor=white)](https://www.docker.com/)

**Статус проекта**

[![CI](https://img.shields.io/badge/CI-passing-brightgreen?style=for-the-badge&logo=github)](https://github.com/positron48/budget/actions/workflows/ci.yml) [![Security Audit](https://img.shields.io/badge/Security%20Audit-passing-brightgreen?style=for-the-badge&logo=github)](https://github.com/positron48/budget/actions/workflows/security.yml) [![Generate Stubs](https://img.shields.io/badge/Generate%20Protobuf%20Stubs-passing-brightgreen?style=for-the-badge&logo=github)](https://github.com/positron48/budget/actions/workflows/generate-stubs.yml) [![Go Report Card](https://img.shields.io/badge/Go%20Report%20Card-A+-brightgreen?style=for-the-badge&logo=go)](https://goreportcard.com/report/github.com/positron48/budget) [![Codecov](https://img.shields.io/badge/coverage-80%25-brightgreen?style=for-the-badge)](https://codecov.io/gh/positron48/budget) [![Dependabot](https://img.shields.io/badge/dependabot-enabled-025e8c?style=for-the-badge&logo=dependabot)](https://dependabot.com/)

**Лицензия и участие**

[![License](https://img.shields.io/badge/License-CC%20BY--NC%204.0-yellow?style=for-the-badge)](LICENSE) [![PRs Welcome](https://img.shields.io/badge/PRs-Welcome-brightgreen?style=for-the-badge)](http://makeapullrequest.com)

**Статистика**

[![Contributors](https://img.shields.io/github/contributors/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/graphs/contributors) [![Last Commit](https://img.shields.io/github/last-commit/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/commits) [![Release](https://img.shields.io/github/release/positron48/budget?style=for-the-badge&logo=github)](https://github.com/positron48/budget/releases)

[English](README_EN.md) | [Русский](README.md)

> Современное веб-приложение для учета личных финансов с поддержкой многопользовательности, импорта/экспорта данных и красивым интерфейсом.

## Быстрый старт

### Предварительные требования
- Docker и Docker Compose
- Git

### Установка и запуск
```bash
# Клонирование репозитория
git clone https://github.com/positron48/budget
cd budget

# Запуск всего окружения
make up

# Проверка статуса
make logs
```

### Доступ к сервисам
- **Frontend**: http://localhost:3030
- **Backend gRPC**: localhost:8080
- **Grafana**: http://localhost:3002
- **Prometheus**: http://localhost:9090

### Первые шаги
1. Откройте http://localhost:3030
2. Зарегистрируйтесь с email/паролем
3. Создайте первую организацию
4. Начните добавлять категории и транзакции

## Основные возможности

<div align="center">

[![Transactions](https://img.shields.io/badge/💳-Transactions-00BCD4?style=for-the-badge)](https://github.com/positron48/budget#-управление-транзакциями)
[![Analytics](https://img.shields.io/badge/📊-Analytics-4CAF50?style=for-the-badge)](https://github.com/positron48/budget#-аналитика-и-отчеты)
[![Import/Export](https://img.shields.io/badge/🔄-Import/Export-FF9800?style=for-the-badge)](https://github.com/positron48/budget#-импортэкспорт-данных)
[![Multi-User](https://img.shields.io/badge/👥-Multi--User-9C27B0?style=for-the-badge)](https://github.com/positron48/budget#-многопользовательность)
[![i18n](https://img.shields.io/badge/🌍-i18n-2196F3?style=for-the-badge)](https://github.com/positron48/budget#-интернационализация)

</div>

### Управление транзакциями
- CRUD операции с транзакциями (доходы/расходы)
- Категоризация с поддержкой i18n
- Фильтрация и поиск с быстрыми фильтрами
- Пагинация и сортировка
- Экспорт в CSV с фильтрами
- Быстрые фильтры (текущий месяц, прошлый месяц, год, 30 дней)

### Аналитика и отчеты
- Месячные отчеты по категориям
- Визуализация данных с графиками
- Сравнение периодов
- Статистика доходов и расходов

### Импорт/Экспорт данных
- Импорт CSV файлов с настройкой маппинга
- Автоматическое определение кодировки
- Предварительный просмотр данных
- Экспорт с учетом всех фильтров

### Многопользовательность
- Multi-tenant архитектура
- Роли: Owner, Admin, Member
- Управление организациями
- Изоляция данных между аккаунтами

### Интернационализация
- Поддержка русского и английского языков
- Локализованные категории
- Автоматическое переключение языков

## Архитектура

```mermaid
flowchart TB
  subgraph Client
    Web["Web App (Next.js + TS)"]
    TG["Telegram Bot (Go)"]
  end

  subgraph Edge
    Envoy["Envoy Proxy<br/>gRPC-Web → gRPC"]
  end

  subgraph Core
    Budgetd["budgetd (Go)<br/>Hexagonal Architecture<br/>– gRPC API<br/>– Domain Services<br/>– Repository Pattern"]
  end

  subgraph Infra
    PG["PostgreSQL 15+"]
    MQ["NATS/RabbitMQ"]
    S3["S3 Storage"]
    OTEL["OpenTelemetry"]
  end

  Web -->|gRPC-Web| Envoy -->|gRPC| Budgetd
  TG -->|gRPC| Budgetd
  Budgetd <-->|SQL| PG
  Budgetd -->|events| MQ
  Budgetd -->|files| S3
  Budgetd --> OTEL
```

## Технологический стек

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

## Развертывание

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

## Структура проекта

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

## Основные команды

| Команда | Описание |
|---------|----------|
| `make up` | Запуск всего окружения |
| `make down` | Остановка всех сервисов |
| `make check` | Проверки backend + frontend |
| `make proto` | Генерация protobuf кода |
| `make migrate-up` | Применение миграций БД |
| `make test` | Запуск тестов |
| `make logs` | Просмотр логов |

## Roadmap

### В разработке
- **Telegram бот** для быстрого добавления транзакций
- **Интеграции с банками** для автоматического импорта
- **Планирование бюджета** и финансовые цели

### Планируется
- **Мобильное приложение** (React Native)
- **E2E тесты** с Playwright
- **Performance оптимизации**
- **PWA функциональность**
- **Оффлайн режим**
- **Push уведомления**

## Вклад в проект

Мы приветствуем вклад в развитие проекта! Пожалуйста, ознакомьтесь с нашими [правилами контрибьюции](CONTRIBUTING.md).

### Как помочь:
1. **Сообщить о баге** - создайте issue
2. **Предложить идею** - создайте feature request
3. **Исправить баг** - создайте pull request
4. **Улучшить документацию** - отредактируйте README

## Лицензия

Этот проект лицензирован под Creative Commons Attribution-NonCommercial 4.0 International License - см. файл [LICENSE](LICENSE) для деталей.

## Благодарности

- [Go](https://golang.org/) - за отличный язык программирования
- [Next.js](https://nextjs.org/) - за современный React фреймворк
- [Tailwind CSS](https://tailwindcss.com/) - за утилитарный CSS
- [gRPC](https://grpc.io/) - за эффективный API протокол

---

**⭐ Если проект вам понравился, поставьте звездочку!**



# Конфигурация Budget Service

## 📋 Обзор

Budget Service состоит из нескольких компонентов:
- **Backend (Go/gRPC)** - порт 8080
- **Frontend (Next.js)** - порт 3030  
- **Envoy Proxy** - порт 8081
- **PostgreSQL** - порт 5432
- **Redis** - порт 6379

## 🔧 Конфигурация

### Переменные окружения

#### Для локальной разработки:
Создайте файл `.env` в корне проекта на основе `env.example`:

```bash
cp env.example .env
```

#### Для Docker Compose:
Используется файл `env.docker` с настройками для контейнеров.

### Основные переменные

#### Backend (Go)
```bash
# База данных
DATABASE_URL=postgres://budget:budget@localhost:5432/budget?sslmode=disable

# Redis
REDIS_URL=redis://localhost:6379

# JWT
JWT_SIGN_KEY=your-secret-key
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL=720h

# gRPC
GRPC_ADDR=0.0.0.0:8080
METRICS_ADDR=0.0.0.0:9090

# OAuth
OAUTH_WEB_BASE_URL=http://localhost:3030
OAUTH_AUTH_TOKEN_TTL=5m
OAUTH_SESSION_TTL=24h
OAUTH_VERIFICATION_CODE_TTL=10m
```

#### Frontend (Next.js)
```bash
# gRPC клиент
NEXT_PUBLIC_GRPC_BASE_URL=http://localhost:3030/grpc

# URL приложения
NEXT_PUBLIC_APP_URL=http://localhost:3030

# Envoy proxy
ENVOY_URL=http://localhost:8081
```

## 🚀 Запуск

### Разработка

```bash
# Backend + база данных
docker compose up -d

# Frontend (отдельно)
cd web
npm run dev
```

### Продакшен

```bash
# Все сервисы
docker compose up -d
```

## 🌍 Окружения

### Development
- Backend: `http://localhost:8080`
- Frontend: `http://localhost:3030`
- Envoy: `http://localhost:8081`

### Production
Обновите переменные в `.env`:
```bash
APP_ENV=production
OAUTH_WEB_BASE_URL=https://your-domain.com
NEXT_PUBLIC_GRPC_BASE_URL=https://your-domain.com/grpc
NEXT_PUBLIC_APP_URL=https://your-domain.com
JWT_SIGN_KEY=your-super-secret-production-key
```

## 🔗 Схема взаимодействия

```
Frontend (3030) → Envoy (8081) → Backend (8080)
     ↓
OAuth ссылки → Frontend (3030)
```

## 📝 Важные моменты

1. **OAuth ссылки** генерируются на `OAUTH_WEB_BASE_URL`
2. **Frontend** должен быть доступен по этому URL
3. **gRPC** запросы идут через Envoy proxy
4. **Порты** должны совпадать в конфигурации

## 🐛 Отладка

### Проверка конфигурации
```bash
# Backend
docker compose logs app

# Frontend  
docker compose logs web

# База данных
docker compose exec db psql -U budget -d budget -c "SELECT version();"
```

### Проверка OAuth
```bash
# Генерация ссылки
grpcurl -plaintext -d '{"email": "test@example.com", "telegramUserId": "123"}' localhost:8080 budget.v1.OAuthService.GenerateAuthLink
```

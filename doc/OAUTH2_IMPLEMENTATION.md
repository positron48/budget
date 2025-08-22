# OAuth2 Flow для Telegram бота - Реализация

## Обзор

Реализована полная система OAuth2 авторизации для Telegram бота, которая позволяет пользователям безопасно авторизоваться через веб-интерфейс без передачи логина и пароля через Telegram.

## Архитектура

### Компоненты

1. **Backend API (Go/gRPC)**
   - OAuth2 сервис с методами генерации ссылок, верификации кодов и управления сессиями
   - Redis для кэширования временных токенов и rate limiting
   - PostgreSQL для хранения сессий и логов безопасности

2. **Веб-интерфейс (Next.js)**
   - Страница авторизации с формой входа
   - API роуты для взаимодействия с backend
   - Адаптивный дизайн для мобильных устройств

3. **Telegram Bot**
   - Управление состояниями авторизации
   - Генерация ссылок и обработка кодов подтверждения

## Безопасность

### Основные принципы

- **Защита данных**: Логин и пароль никогда не передаются через Telegram
- **Временные токены**: Короткий срок жизни (5 минут) для авторизации
- **Одноразовые коды**: 6-значные коды подтверждения
- **Rate limiting**: Ограничение попыток авторизации
- **Логирование**: Полный аудит всех операций

### Защита от атак

- CSRF защита на веб-формах
- XSS защита в веб-интерфейсе
- SQL инъекции - параметризованные запросы
- Брутфорс атаки - ограничение попыток входа
- Автоматическая блокировка при подозрительной активности

## Установка и настройка

### 1. Зависимости

Добавьте в `go.mod`:
```go
github.com/redis/go-redis/v9 v9.5.1
```

### 2. Переменные окружения

```bash
# Redis
REDIS_URL=redis://localhost:6379

# OAuth2 конфигурация
OAUTH_AUTH_TOKEN_TTL=5m
OAUTH_SESSION_TTL=24h
OAUTH_VERIFICATION_CODE_TTL=10m
OAUTH_MAX_ATTEMPTS_PER_HOUR=10
OAUTH_MAX_ATTEMPTS_PER_10MIN=3
OAUTH_WEB_BASE_URL=http://localhost:3000
```

### 3. База данных

Запустите миграции:
```bash
migrate -path migrations -database "postgres://..." up
```

### 4. Redis

Установите и запустите Redis:
```bash
# Ubuntu/Debian
sudo apt install redis-server

# macOS
brew install redis

# Запуск
redis-server
```

## Использование

### 1. Генерация ссылки авторизации

```go
// В Telegram боте
authURL, authToken, expiresAt, err := oauthService.GenerateAuthLink(
    ctx, 
    email, 
    telegramUserID, 
    userAgent, 
    ipAddress,
)
```

### 2. Отправка ссылки пользователю

```go
// Отправляем пользователю ссылку для авторизации
bot.Send(tgbotapi.NewMessage(chatID, 
    fmt.Sprintf("Для авторизации перейдите по ссылке: %s", authURL)))
```

### 3. Ожидание кода подтверждения

```go
// Пользователь вводит код в боте
verificationCode := "123456" // Получен от пользователя

tokenPair, sessionID, err := oauthService.VerifyAuthCode(
    ctx, 
    authToken, 
    verificationCode, 
    telegramUserID,
)
```

### 4. Использование сессии

```go
// Получение активной сессии
session, err := oauthService.GetTelegramSession(ctx, sessionID)
if err != nil {
    // Обработка ошибки
}

// Использование токенов для API вызовов
// session содержит access_token_hash и refresh_token_hash
```

## API Endpoints

### gRPC API

```protobuf
service OAuthService {
  // Генерация ссылки авторизации
  rpc GenerateAuthLink(GenerateAuthLinkRequest) returns (GenerateAuthLinkResponse);
  
  // Верификация кода подтверждения
  rpc VerifyAuthCode(VerifyAuthCodeRequest) returns (VerifyAuthCodeResponse);
  
  // Отмена авторизации
  rpc CancelAuth(CancelAuthRequest) returns (CancelAuthResponse);
  
  // Получение статуса авторизации
  rpc GetAuthStatus(GetAuthStatusRequest) returns (GetAuthStatusResponse);
  
  // Управление сессиями
  rpc GetTelegramSession(GetTelegramSessionRequest) returns (GetTelegramSessionResponse);
  rpc RevokeTelegramSession(RevokeTelegramSessionRequest) returns (RevokeTelegramSessionResponse);
  rpc ListTelegramSessions(ListTelegramSessionsRequest) returns (ListTelegramSessionsResponse);
  
  // Логи безопасности
  rpc GetAuthLogs(GetAuthLogsRequest) returns (GetAuthLogsResponse);
}
```

### HTTP API (для веб-интерфейса)

- `GET /api/oauth/status?token=<token>` - Проверка статуса токена
- `POST /api/oauth/login` - Авторизация пользователя
- `POST /api/oauth/verify` - Верификация кода
- `POST /api/oauth/cancel` - Отмена авторизации

## Пользовательский сценарий

### 1. Инициация авторизации

1. Пользователь отправляет `/login` в Telegram боте
2. Бот запрашивает email пользователя
3. Пользователь вводит email
4. Бот генерирует уникальную ссылку для авторизации

### 2. Процесс авторизации

1. Пользователь переходит по ссылке в браузере
2. На веб-странице вводит логин и пароль от основного сервиса
3. После успешной авторизации показывается 6-значный код подтверждения
4. Пользователь вводит код в Telegram боте
5. Бот подтверждает успешную авторизацию

### 3. Использование

1. Бот получает доступ к функциям пользователя
2. Создается сессия для последующих запросов
3. Пользователь может управлять своими сессиями

## Мониторинг и безопасность

### Логирование

Все операции логируются с информацией:
- IP адрес
- User-Agent
- Email пользователя
- Telegram User ID
- Действие (generate_link, verify_code, cancel, revoke_session)
- Статус (success, failed, expired)
- Время выполнения

### Rate Limiting

- Максимум 3 попытки генерации ссылки за 10 минут
- Максимум 10 попыток верификации кода в час
- Автоматическая блокировка при превышении лимитов

### Очистка данных

- Автоматическое удаление истекших токенов
- Очистка старых сессий
- Удаление устаревших записей rate limiting

## Тестирование

### Unit тесты

```bash
go test ./internal/usecase/oauth/...
go test ./internal/adapter/postgres/...
go test ./internal/adapter/redis/...
```

### Интеграционные тесты

```bash
go test ./internal/adapter/grpc/...
```

### E2E тесты

```bash
# Тестирование веб-интерфейса
npm run test:e2e
```

## Развертывание

### Docker Compose

```yaml
version: '3.8'
services:
  app:
    build: .
    environment:
      - REDIS_URL=redis://redis:6379
      - OAUTH_WEB_BASE_URL=https://your-domain.com
    depends_on:
      - postgres
      - redis

  postgres:
    image: postgres:15
    environment:
      - POSTGRES_DB=budget
      - POSTGRES_USER=budget
      - POSTGRES_PASSWORD=password

  redis:
    image: redis:7-alpine
```

### Production

1. Настройте SSL/TLS для веб-интерфейса
2. Используйте внешний Redis (например, AWS ElastiCache)
3. Настройте мониторинг и алерты
4. Регулярно обновляйте зависимости
5. Настройте резервное копирование данных

## Устранение неполадок

### Частые проблемы

1. **Токен истек**: Увеличьте `OAUTH_AUTH_TOKEN_TTL`
2. **Rate limit exceeded**: Проверьте настройки лимитов
3. **Redis недоступен**: Проверьте подключение к Redis
4. **Ошибки верификации**: Проверьте логи безопасности

### Логи

```bash
# Просмотр логов приложения
docker logs budget-app

# Просмотр логов Redis
docker logs budget-redis

# Просмотр логов PostgreSQL
docker logs budget-postgres
```

## Дальнейшее развитие

### Планируемые улучшения

1. **Magic Link**: Авторизация через email без пароля
2. **2FA**: Двухфакторная аутентификация
3. **SSO**: Интеграция с внешними провайдерами
4. **Аналитика**: Детальная статистика использования
5. **Уведомления**: Push-уведомления о новых сессиях

### API версионирование

- Поддержка нескольких версий API
- Обратная совместимость
- Миграция данных между версиями

## Поддержка

При возникновении проблем:

1. Проверьте логи приложения
2. Убедитесь в корректности конфигурации
3. Проверьте доступность зависимостей (Redis, PostgreSQL)
4. Обратитесь к документации по API
5. Создайте issue в репозитории проекта

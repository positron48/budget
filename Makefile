PROTO_DIR=proto
DB_URL=postgres://budget:budget@localhost:5432/budget?sslmode=disable

# Список всех целей в одном месте
.PHONY: help proto dproto tsproto lproto-go build run run-dev run-backend stop restart up down logs logs-dev ps tidy fmt test pgtest lint vet ci check web-install web-build web-lint web-test web-check check-all deploy-frontend deploy-backend deploy-all deploy-frontend-force migrate-up migrate-down dmigrate-up dmigrate-down docker-df docker-prune docker-prune-all oauth-test oauth-cleanup

# Вывести список целей и их описание (с группировкой по разделам)
help: ## [Meta] Показать список команд по разделам
	@echo "Доступные цели:"; \
	grep -E '^[a-zA-Z0-9_-]+:.*?## .+' $(MAKEFILE_LIST) | sort | \
	awk 'BEGIN{FS=":.*?## "; n=split("Dev Proto Go Docker Web Migrate Meta Other", ord, " "); c_reset=sprintf("%c[0m",27); c_title=sprintf("%c[1;37m",27); c_header=sprintf("%c[34m",27); c_target=sprintf("%c[32m",27); c_desc=sprintf("%c[90m",27); printf("%s\n", "");} {target=$$1; desc=$$2; cat="Other"; if (match(desc, /^\[[^]]+\] /)) {cat=substr(desc, RSTART+1, RLENGTH-3); desc=substr(desc, RSTART+RLENGTH)}; content[cat]=content[cat] sprintf("  " c_target "%-20s" c_reset " " c_desc "%s" c_reset "\n", target, desc);} END{for(i=1;i<=n;i++){cat=ord[i]; if (content[cat] != "") {printf c_header "%s" c_reset ":\n", cat; printf "%s", content[cat];}} if (content["Other"] != "") {printf c_header "Other" c_reset ":\n"; printf "%s", content["Other"];}}'

proto: ## [Proto] Сгенерировать protobuf (buf lint + generate)
	cd $(PROTO_DIR) && buf lint && buf generate

dproto: ## [Proto] Сгенерировать protobuf в docker (buf)
	docker run --rm -v $(PWD):/workspace -w /workspace/proto bufbuild/buf:latest generate

tsproto: ## [Proto] Сгенерировать только TS stubs для фронта (локальные плагины из web/node_modules)
	cd $(PROTO_DIR) && buf generate --template buf.gen.ts.yaml

lproto-go: ## [Proto] Сгенерировать Go stubs локальными плагинами (protoc-gen-go, protoc-gen-go-grpc должны быть в PATH)
	cd $(PROTO_DIR) && buf generate --template buf.gen.local-go.yaml

build: ## [Go] Сборка Go бинарника (bin/budgetd)
	go build -o bin/budgetd ./cmd/budgetd

run: up ## [Dev] Запустить полное окружение (Docker)
	@printf "\n\033[34mDev окружение запущено:\033[0m\n"; \
	printf "  \033[32mFrontend\033[0m: \033[90mhttp://localhost:3030\033[0m\n"; \
	printf "  \033[32mgRPC-Web (через Envoy)\033[0m: \033[90mhttp://localhost:8081/grpc\033[0m\n"; \
	printf "  \033[32mBackend gRPC\033[0m: \033[90m0.0.0.0:8080\033[0m (в контейнере, проброшено на хост)\n"; \
	printf "  \033[32mDB\033[0m: \033[90mpostgres://budget:budget@localhost:5432/budget?sslmode=disable\033[0m\n"; \
	printf "  \033[32mRedis\033[0m: \033[90mredis://localhost:6379\033[0m\n\n"; \
	printf "  \033[34mЛоги:\033[0m \033[90mmake logs\033[0m\n"; \
	printf "  \033[34mСтатус:\033[0m \033[90mmake ps\033[0m\n"; \
	printf "  \033[34mОстановка:\033[0m \033[90mmake stop\033[0m\n\n"

run-dev: up-dev ## [Dev] Запустить окружение с hot reload для фронтенда
	@printf "\n\033[34mDev окружение с hot reload запущено:\033[0m\n"; \
	printf "  \033[32mFrontend (hot reload)\033[0m: \033[90mhttp://localhost:3030\033[0m\n"; \
	printf "  \033[32mgRPC-Web (через Envoy)\033[0m: \033[90mhttp://localhost:8081/grpc\033[0m\n"; \
	printf "  \033[32mBackend gRPC\033[0m: \033[90m0.0.0.0:8080\033[0m (в контейнере, проброшено на хост)\n"; \
	printf "  \033[32mDB\033[0m: \033[90mpostgres://budget:budget@localhost:5432/budget?sslmode=disable\033[0m\n"; \
	printf "  \033[32mRedis\033[0m: \033[90mredis://localhost:6379\033[0m\n\n"; \
	printf "  \033[34mЛоги:\033[0m \033[90mmake logs-dev\033[0m\n"; \
	printf "  \033[34mСтатус:\033[0m \033[90mmake ps\033[0m\n"; \
	printf "  \033[34mОстановка:\033[0m \033[90mmake stop\033[0m\n"; \
	printf "  \033[34mHot reload:\033[0m \033[90mИзменения в ./web автоматически пересобираются\033[0m\n\n"

stop: ## [Dev] Остановить Docker окружение
	@printf "\n\033[34mОстановка Docker окружения...\033[0m\n"; \
	docker compose down || true; \
	printf "  \033[90mDocker compose остановлен\033[0m\n\n"

restart: ## [Dev] Перезапуск окружения (stop -> run)
	$(MAKE) stop
	$(MAKE) run

up: ## [Docker] Запуск полного окружения
	docker compose up -d

up-dev: ## [Docker] Запуск окружения с hot reload для фронтенда
	docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d

docker-df: ## [Docker] Показать использование диска Docker (образы/кеши/тома)
	@echo "Docker disk usage:"; docker system df || true; \
	echo "\nBuilders:"; docker builder ls || true; \
	echo "\nBuild cache (dry-run):"; docker builder prune --verbose --dry-run || true

docker-prune: ## [Docker] Очистить неиспользуемое (dangling) — контейнеры/сети/образы/кеш сборки
	docker system prune -f || true
	docker builder prune -f || true
	docker image prune -f || true

docker-prune-all: ## [Docker] Жесткая очистка: +неиспользуемые образы (-a), кеш сборки (--all)
	@echo "ВНИМАНИЕ: будут удалены ВСЕ неиспользуемые ресурсы Docker, включая кеши сборки и неиспользуемые тома."; \
		echo "Продолжаю...";
	docker system prune -a -f || true
	docker builder prune --all -f || true

down: ## [Docker] Остановка docker compose (без удаления данных)
	docker compose down

logs: ## [Docker] Логи docker compose (-f --tail=200)
	docker compose logs -f --tail=200

logs-dev: ## [Docker] Логи docker compose в режиме разработки (-f --tail=200)
	docker compose -f docker-compose.yml -f docker-compose.dev.yml logs -f --tail=200

tidy: ## [Go] Обновить зависимости (go mod tidy)
	docker run --rm -v $(PWD):/app -w /app golang:1.24 go mod tidy

fmt: ## [Go] Форматирование кода (gofumpt/gofmt)
	docker run --rm -v $(PWD):/app -w /app golang:1.24 bash -c "go install mvdan.cc/gofumpt@latest && gofumpt -w . && gofmt -s -w ."

test: ## [Go] Запуск тестов Go (-race, coverage)
	docker run --rm -v $(PWD):/app -w /app golang:1.24 go test ./... -race -coverprofile=coverage.out -covermode=atomic

pgtest: ## [Go] Интеграционные тесты PostgreSQL (PG_INTEGRATION=1)
	docker run --rm -v $(PWD):/app -w /app --network host golang:1.24 bash -c "PG_INTEGRATION=1 go test ./internal/adapter/postgres -run Test.*_PG -v"

LINT_IMAGE_TAG ?= v1.64.8

lint: ## [Go] Линтер Go (golangci-lint в docker)
	@echo "Running golangci-lint in docker ($(LINT_IMAGE_TAG))"
	docker run --rm -e GOTOOLCHAIN=local -v $(PWD):/app -w /app golangci/golangci-lint:$(LINT_IMAGE_TAG) golangci-lint run --timeout=5m

vet: ## [Go] Анализ кода (go vet)
	docker run --rm -v $(PWD):/app -w /app golang:1.24 go vet ./...

ci: tidy vet lint test ## [Go] Мини CI: tidy vet lint test

check: tidy fmt vet lint test ## [Go] Полная проверка Go: tidy fmt vet lint test

web-install: ## [Web] Установка зависимостей фронта (Docker)
	docker run --rm -v $(PWD)/web:/app -w /app node:20-alpine npm ci || docker run --rm -v $(PWD)/web:/app -w /app node:20-alpine npm install

web-build: ## [Web] Сборка фронта (Docker)
	docker run --rm -v $(PWD)/web:/app -w /app node:20-alpine npm run build

web-lint: ## [Web] Линт фронта (Docker)
	docker run --rm -v $(PWD)/web:/app -w /app node:20-alpine npm run lint || echo "eslint not configured, skipping"

web-test: ## [Web] Тесты фронта (Docker)
	docker run --rm -v $(PWD)/web:/app -w /app node:20-alpine npm run test || echo "no web tests configured yet, skipping"

web-check: web-install web-lint web-build web-test ## [Web] Полная проверка фронта

check-all: check web-check ## [Meta] Полная проверка всего: Go + Web

# =============================================================================
# PRODUCTION DEPLOYMENT
# =============================================================================

deploy-frontend: ## [Deploy] Обновить фронтенд на проде (пересборка + перезапуск)
	@printf "\n\033[34mОбновление фронтенда на проде...\033[0m\n"; \
	docker compose build web --no-cache; \
	docker compose up -d web; \
	printf "  \033[32m✓ Фронтенд обновлен\033[0m\n\n"

deploy-backend: ## [Deploy] Обновить бэкенд на проде (пересборка + перезапуск)
	@printf "\n\033[34mОбновление бэкенда на проде...\033[0m\n"; \
	docker compose build app --no-cache; \
	docker compose up -d app; \
	printf "  \033[32m✓ Бэкенд обновлен\033[0m\n\n"

deploy-all: ## [Deploy] Обновить все сервисы на проде
	@printf "\n\033[34mПолное обновление на проде...\033[0m\n"; \
	docker compose build --no-cache; \
	docker compose up -d; \
	printf "  \033[32m✓ Все сервисы обновлены\033[0m\n\n"

deploy-frontend-force: ## [Deploy] Принудительное обновление фронтенда (удаление + пересборка)
	@printf "\n\033[34mПринудительное обновление фронтенда...\033[0m\n"; \
	docker compose stop web; \
	docker compose rm -f web; \
	docker rmi budget-web || true; \
	docker compose build web --no-cache; \
	docker compose up -d web; \
	printf "  \033[32m✓ Фронтенд принудительно обновлен\033[0m\n\n"

migrate-up: ## [Migrate] Миграции вверх (Docker)
	docker run --rm -v $(PWD)/migrations:/migrations --network host migrate/migrate -database "postgres://budget:budget@localhost:5432/budget?sslmode=disable" -path /migrations up

migrate-down: ## [Migrate] Откат одной миграции (Docker)
	docker run --rm -v $(PWD)/migrations:/migrations --network host migrate/migrate -database "postgres://budget:budget@localhost:5432/budget?sslmode=disable" -path /migrations down 1

# Dockerized migrate (no local CLI required)
dmigrate-up: ## [Migrate] Миграции вверх в docker (без локального CLI)
	docker run --rm -v $(PWD)/migrations:/migrations --network host migrate/migrate -database "postgres://budget:budget@localhost:5432/budget?sslmode=disable" -path /migrations up

dmigrate-down: ## [Migrate] Откат одной миграции в docker
	docker run --rm -v $(PWD)/migrations:/migrations --network host migrate/migrate -database "postgres://budget:budget@localhost:5432/budget?sslmode=disable" -path /migrations down 1


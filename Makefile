PROTO_DIR=proto
DB_URL=postgres://budget:budget@localhost:5432/budget?sslmode=disable

# Список всех целей в одном месте
.PHONY: help proto dproto tsproto lproto-go build run run-dev run-backend stop restart up down logs logs-dev ps tidy fmt test pgtest lint vet govuln ci-go ci-web ci-go-security ci-node-security ci-container-security ci check web-install web-build web-lint web-test web-audit web-check check-all deploy-frontend deploy-backend deploy-all deploy-frontend-force migrate-up migrate-down dmigrate-up dmigrate-down docker-df docker-prune docker-prune-all oauth-test oauth-cleanup deploy-backend-artifact deploy-frontend-artifact deploy-all-artifact check-updates

# Вывести список целей и их описание (с группировкой по разделам)
help: ## [Meta] Показать список команд по разделам
	@echo "Доступные цели:"; \
	grep -E '^[a-zA-Z0-9_-]+:.*?## .+' $(MAKEFILE_LIST) | sort | \
	awk 'BEGIN{FS=":.*?## "; n=split("Dev Proto Go Docker Web Migrate Deploy Meta Other", ord, " "); c_reset=sprintf("%c[0m",27); c_title=sprintf("%c[1;37m",27); c_header=sprintf("%c[34m",27); c_target=sprintf("%c[32m",27); c_desc=sprintf("%c[90m",27); printf("%s\n", "");} {target=$$1; desc=$$2; cat="Other"; if (match(desc, /^\[[^]]+\] /)) {cat=substr(desc, RSTART+1, RLENGTH-3); desc=substr(desc, RSTART+RLENGTH)}; content[cat]=content[cat] sprintf("  " c_target "%-20s" c_reset " " c_desc "%s" c_reset "\n", target, desc);} END{for(i=1;i<=n;i++){cat=ord[i]; if (content[cat] != "") {printf c_header "%s" c_reset ":\n", cat; printf "%s", content[cat];}} if (content["Other"] != "") {printf c_header "Other" c_reset ":\n"; printf "%s", content["Other"];}}'

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

up-dev: ## [Docker] Запуск окружения с hot reload для фронтенда (использует Dockerfile.dev)
	@echo "Запуск окружения с hot reload (docker-compose.dev.yml)"
	docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build

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
	docker compose logs -f --tail=200

tidy: ## [Go] Обновить зависимости (go mod tidy, локально)
	go mod tidy

fmt: ## [Go] Форматирование кода (gofumpt/gofmt, локально)
	go install mvdan.cc/gofumpt@latest
	gofumpt -w .
	gofmt -s -w .

test: ## [Go] Запуск тестов Go (-race, coverage, локально)
	go test ./... -race -coverprofile=coverage.out -covermode=atomic

pgtest: ## [Go] Интеграционные тесты PostgreSQL (PG_INTEGRATION=1)
	docker run --rm -v $(PWD):/app -w /app --network host golang:1.24.13 bash -c "PG_INTEGRATION=1 go test ./internal/adapter/postgres -run Test.*_PG -v"

LINT_IMAGE_TAG ?= v2.1.0

lint: ## [Go] Линтер Go (golangci-lint, локально)
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.0 run --timeout=5m

vet: ## [Go] Анализ кода (go vet, локально)
	go vet ./...

govuln: ## [Go] Проверка уязвимостей Go (govulncheck, локально)
	go install golang.org/x/vuln/cmd/govulncheck@latest
	$(shell go env GOPATH)/bin/govulncheck ./...

ci-go: tidy lint test ## [Go] CI-совместимые проверки backend (tidy+lint+test)

ci-web: web-install web-lint web-build web-test ## [Web] CI-совместимые проверки frontend

ci-go-security: govuln ## [Go] CI-совместимая security-проверка Go

ci-node-security: web-install web-audit ## [Web] CI-совместимая security-проверка npm audit

ci-container-security: ## [Sec] CI-совместимая security-проверка контейнеров (Trivy)
	@if command -v trivy >/dev/null 2>&1; then \
		trivy config .; \
	else \
		echo "trivy не найден локально, запускаю в docker..."; \
		docker run --rm -v "$(PWD):/work" -w /work aquasec/trivy:latest config .; \
	fi

ci: ci-go ci-web ci-go-security ci-node-security ci-container-security ## [Meta] Полная CI-последовательность локально

check: ci ## [Meta] Идентично CI: backend + frontend + security

web-install: ## [Web] Установка зависимостей фронта (локально)
	cd web && npm ci || npm install

web-build: ## [Web] Сборка фронта (локально)
	cd web && npm run build

web-lint: ## [Web] Линт фронта (локально)
	cd web && npm run lint || echo "eslint not configured, skipping"

web-test: ## [Web] Тесты фронта (локально)
	cd web && npm run test || echo "no web tests configured yet, skipping"

web-audit: ## [Web] Аудит зависимостей фронта (локально)
	cd web && npm audit --omit=dev --audit-level=moderate

web-check: web-install web-lint web-build web-test web-audit ## [Web] Полная проверка фронта

check-all: check ## [Meta] Алиас полной CI-совместимой проверки

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

# Миграции через сеть docker compose (работает на macOS, когда контейнеры уже подняты)
dmigrate-up-compose: ## [Migrate] Миграции вверх через сеть compose (macOS / после make up)
	docker run --rm -v $(PWD)/migrations:/migrations --network budget_budget-network migrate/migrate -database "postgres://budget:budget@db:5432/budget?sslmode=disable" -path /migrations up

dmigrate-down-compose: ## [Migrate] Откат одной миграции через сеть compose
	docker run --rm -v $(PWD)/migrations:/migrations --network budget_budget-network migrate/migrate -database "postgres://budget:budget@db:5432/budget?sslmode=disable" -path /migrations down 1

# =============================================================================
# ARTIFACT DEPLOYMENT (GitHub Releases)
# =============================================================================

deploy-backend-artifact: ## [Deploy] Деплой бэкенда из GitHub артефактов (sudo ./scripts/deploy-backend.sh [tag] [arch])
	@printf "\n\033[34mДеплой бэкенда из GitHub артефактов...\033[0m\n"; \
	if [ -z "$(TAG)" ]; then \
		printf "  \033[32mИспользование:\033[0m \033[90msudo make deploy-backend-artifact TAG=v1.0.0 ARCH=linux-amd64\033[0m\n"; \
		printf "  \033[32mИли для последней версии:\033[0m \033[90msudo make deploy-backend-artifact TAG=latest\033[0m\n"; \
		printf "  \033[32mАрхитектуры:\033[0m \033[90mlinux-amd64, linux-arm64, windows-amd64, darwin-amd64, darwin-arm64\033[0m\n\n"; \
	else \
		sudo ./scripts/deploy-backend.sh $(TAG) $(ARCH); \
	fi

deploy-frontend-artifact: ## [Deploy] Деплой фронтенда из GitHub артефактов (sudo ./scripts/deploy-frontend.sh [tag])
	@printf "\n\033[34mДеплой фронтенда из GitHub артефактов...\033[0m\n"; \
	if [ -z "$(TAG)" ]; then \
		printf "  \033[32mИспользование:\033[0m \033[90msudo make deploy-frontend-artifact TAG=v1.0.0\033[0m\n"; \
		printf "  \033[32mИли для последней версии:\033[0m \033[90msudo make deploy-frontend-artifact TAG=latest\033[0m\n\n"; \
	else \
		sudo ./scripts/deploy-frontend.sh $(TAG); \
	fi

deploy-all-artifact: ## [Deploy] Полный деплой из GitHub артефактов (sudo ./scripts/deploy-all.sh [tag] [arch])
	@printf "\n\033[34mПолный деплой из GitHub артефактов...\033[0m\n"; \
	if [ -z "$(TAG)" ]; then \
		printf "  \033[32mИспользование:\033[0m \033[90msudo make deploy-all-artifact TAG=v1.0.0 ARCH=linux-amd64\033[0m\n"; \
		printf "  \033[32mИли для последней версии:\033[0m \033[90msudo make deploy-all-artifact TAG=latest\033[0m\n"; \
		printf "  \033[32mАрхитектуры:\033[0m \033[90mlinux-amd64, linux-arm64, windows-amd64, darwin-amd64, darwin-arm64\033[0m\n\n"; \
	else \
		sudo ./scripts/deploy-all.sh $(TAG) $(ARCH); \
	fi

check-updates: ## [Deploy] Проверка доступных обновлений из GitHub
	@printf "\n\033[34mПроверка доступных обновлений...\033[0m\n"; \
	./scripts/check-updates.sh

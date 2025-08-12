PROTO_DIR=proto
DB_URL=postgres://budget:budget@localhost:5432/budget?sslmode=disable

# Список всех целей в одном месте
.PHONY: help proto dproto tsproto build run run-backend stop up down logs ps tidy fmt test pgtest lint vet ci check web-install web-build web-lint web-test web-check check-all migrate-up migrate-down dmigrate-up dmigrate-down

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

build: ## [Go] Сборка Go бинарника (bin/budgetd)
	go build -o bin/budgetd ./cmd/budgetd

run: up web-install ## [Dev] Запустить бэкенд (docker) и фронтенд (Next dev)
	@printf "\n\033[34mDev окружение запущено:\033[0m\n"; \
	printf "  \033[32mFrontend\033[0m: \033[90mhttp://localhost:3000\033[0m\n"; \
	printf "  \033[32mgRPC-Web (через Envoy)\033[0m: \033[90mhttp://localhost:8081/grpc\033[0m\n"; \
	printf "  \033[32mBackend gRPC\033[0m: \033[90m0.0.0.0:8080\033[0m (в контейнере, проброшено на хост)\n"; \
	printf "  \033[32mRewrites\033[0m: \033[90mweb/next.config.ts\033[0m — /grpc -> localhost:8081\n"; \
	printf "  \033[32mDB\033[0m: \033[90mpostgres://budget:budget@localhost:5432/budget?sslmode=disable\033[0m\n\n"; \
	if command -v screen >/dev/null 2>&1; then \
	  screen -S budget-web -X quit >/dev/null 2>&1 || true; \
	  screen -dmS budget-web bash -lc 'cd web && npm run dev'; \
	  printf "\033[34m[screen]\033[0m Сессия: \033[32mbudget-web\033[0m\n"; \
	  printf "  Подключиться: \033[90mscreen -r budget-web\033[0m\n"; \
	  printf "  Отсоединиться: \033[90mCtrl-A, D\033[0m\n"; \
	  printf "  Остановить: \033[90mscreen -S budget-web -X quit\033[0m\n\n"; \
	else \
	  printf "\033[33m(screen не найден)\033[0m Запускаю фронтенд через nohup...\n"; \
	  cd web && nohup npm run dev >/dev/null 2>&1 & echo $$! > .next-dev.pid; \
	  printf "  PID: \033[90m$$(cat web/.next-dev.pid)\033[0m\n"; \
	  printf "  Остановить: \033[90mkill $$(cat web/.next-dev.pid)\033[0m\n\n"; \
	fi

stop: ## [Dev] Остановить фронтенд dev (screen/nohup) и docker compose
	@printf "\n\033[34mОстановка dev окружения...\033[0m\n"; \
	if command -v screen >/dev/null 2>&1; then \
	  if screen -list | grep -q \.budget-web; then \
	    screen -S budget-web -X quit || true; \
	    printf "  \033[32mЗакрыта screen-сессия\033[0m: budget-web\n"; \
	  fi; \
	fi; \
	if [ -f web/.next-dev.pid ]; then \
	  PID=$$(cat web/.next-dev.pid); \
	  if kill $$PID >/dev/null 2>&1; then printf "  \033[32mОстановлен фронтенд по PID\033[0m: $$PID\n"; fi; \
	  rm -f web/.next-dev.pid; \
	fi; \
	docker compose down || true; \
	printf "  \033[90mDocker compose остановлен\033[0m\n\n"

run-backend: ## [Go] Запуск только бэкенда локально (go run)
	GRPC_ADDR=0.0.0.0:8080 go run ./cmd/budgetd

up: ## [Docker] Запуск окружения через docker compose (-d --build)
	docker compose up -d

down: ## [Docker] Остановка docker compose (без удаления данных)
	docker compose down

logs: ## [Docker] Логи docker compose (-f --tail=200)
	docker compose logs -f --tail=200

ps: ## [Docker] Список контейнеров docker compose
	docker compose ps

tidy: ## [Go] Обновить зависимости (go mod tidy)
	go mod tidy

fmt: ## [Go] Форматирование кода (gofumpt/gofmt)
	@GOFILES="$(shell git ls-files '*.go')"; \
	if command -v gofumpt >/dev/null 2>&1; then \
	  if [ -n "$$GOFILES" ]; then gofumpt -w $$GOFILES; fi; \
	else \
	  echo "gofumpt not found, skipping"; \
	fi; \
	if [ -n "$$GOFILES" ]; then gofmt -s -w $$GOFILES; fi

test: ## [Go] Запуск тестов Go (-race, coverage)
	go test ./... -race -coverprofile=coverage.out -covermode=atomic

pgtest: ## [Go] Интеграционные тесты PostgreSQL (PG_INTEGRATION=1)
	PG_INTEGRATION=1 go test ./internal/adapter/postgres -run Test.*_PG -v

LINT_IMAGE_TAG ?= v1.64.8

lint: ## [Go] Линтер Go (golangci-lint в docker)
	@echo "Running golangci-lint in docker ($(LINT_IMAGE_TAG))"
	docker run --rm -e GOTOOLCHAIN=local -v $(PWD):/app -w /app golangci/golangci-lint:$(LINT_IMAGE_TAG) golangci-lint run --timeout=5m

vet: ## [Go] Анализ кода (go vet)
	go vet ./...

ci: tidy vet lint test ## [Go] Мини CI: tidy vet lint test

check: tidy fmt vet lint test ## [Go] Полная проверка Go: tidy fmt vet lint test

web-install: ## [Web] Установка зависимостей фронта (npm ci | npm install)
	cd web && npm ci || npm install

web-build: ## [Web] Сборка фронта
	cd web && npm run build

web-lint: ## [Web] Линт фронта (eslint)
	cd web && npm run lint || echo "eslint not configured, skipping"

web-test: ## [Web] Тесты фронта (vitest)
	cd web && npm run test || echo "no web tests configured yet, skipping"

web-check: web-install web-lint web-build web-test ## [Web] Полная проверка фронта

check-all: check web-check ## [Meta] Полная проверка всего: Go + Web

migrate-up: ## [Migrate] Миграции вверх (локальный migrate CLI)
	migrate -database "$(DB_URL)" -path migrations up

migrate-down: ## [Migrate] Откат одной миграции (локальный migrate CLI)
	migrate -database "$(DB_URL)" -path migrations down 1

# Dockerized migrate (no local CLI required)
dmigrate-up: ## [Migrate] Миграции вверх в docker (без локального CLI)
	docker run --rm -v $(PWD)/migrations:/migrations --network host migrate/migrate -database "$(DB_URL)" -path /migrations up

dmigrate-down: ## [Migrate] Откат одной миграции в docker
	docker run --rm -v $(PWD)/migrations:/migrations --network host migrate/migrate -database "$(DB_URL)" -path /migrations down 1


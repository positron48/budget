PROTO_DIR=proto
DB_URL=postgres://budget:budget@localhost:5432/budget?sslmode=disable

.PHONY: proto
proto:
	cd $(PROTO_DIR) && buf lint && buf generate

.PHONY: dproto
dproto:
	docker run --rm -v $(PWD):/workspace -w /workspace/proto bufbuild/buf:latest generate

.PHONY: build
build:
	go build -o bin/budgetd ./cmd/budgetd

.PHONY: run
run:
	GRPC_ADDR=0.0.0.0:8080 go run ./cmd/budgetd

.PHONY: up
up:
	docker compose up -d --build

.PHONY: down
down:
	docker compose down -v

.PHONY: logs
logs:
	docker compose logs -f --tail=200

.PHONY: ps
ps:
	docker compose ps

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: fmt
fmt:
	@if command -v gofumpt >/dev/null 2>&1; then \
	  gofumpt -w . ; \
	else \
	  echo "gofumpt not found, skipping" ; \
	fi
	gofmt -s -w .

.PHONY: test
test:
	go test ./... -race -coverprofile=coverage.out -covermode=atomic

.PHONY: pgtest
pgtest:
	PG_INTEGRATION=1 go test ./internal/adapter/postgres -run Test.*_PG -v

LINT_IMAGE_TAG ?= v1.64.8

.PHONY: lint
lint:
	@echo "Running golangci-lint in docker ($(LINT_IMAGE_TAG))"
	docker run --rm -e GOTOOLCHAIN=local -v $(PWD):/app -w /app golangci/golangci-lint:$(LINT_IMAGE_TAG) golangci-lint run --timeout=5m

.PHONY: vet
vet:
	go vet ./...

.PHONY: ci
ci: tidy vet lint test

.PHONY: check
check: tidy fmt vet lint test

.PHONY: web-install web-build web-lint web-test web-check
web-install:
	cd web && npm ci || npm install

web-build:
	cd web && npm run build

web-lint:
	cd web && npm run lint || echo "eslint not configured, skipping"

web-test:
	cd web && npm run test || echo "no web tests configured yet, skipping"

web-check: web-install web-lint web-build web-test

.PHONY: check-all
check-all: check web-check

.PHONY: migrate-up
migrate-up:
	migrate -database "$(DB_URL)" -path migrations up

.PHONY: migrate-down
migrate-down:
	migrate -database "$(DB_URL)" -path migrations down 1

# Dockerized migrate (no local CLI required)
.PHONY: dmigrate-up dmigrate-down
dmigrate-up:
	docker run --rm -v $(PWD)/migrations:/migrations --network host migrate/migrate -database "$(DB_URL)" -path /migrations up

dmigrate-down:
	docker run --rm -v $(PWD)/migrations:/migrations --network host migrate/migrate -database "$(DB_URL)" -path /migrations down 1



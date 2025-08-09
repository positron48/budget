PROTO_DIR=proto
DB_URL=postgres://budget:budget@localhost:5432/budget?sslmode=disable

.PHONY: proto
proto:
	cd $(PROTO_DIR) && buf lint && buf generate

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

.PHONY: migrate-up
migrate-up:
	migrate -database "$(DB_URL)" -path migrations up

.PHONY: migrate-down
migrate-down:
	migrate -database "$(DB_URL)" -path migrations down 1



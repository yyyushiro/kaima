COMPOSE_FILE := docker-compose.dev.yml
COMPOSE := docker compose -f $(COMPOSE_FILE)

BACKEND_DIR := backend
BACKEND_BIN := $(CURDIR)/bin/backend

.PHONY: dev-up dev-build down logs clean ps migrate-up migrate-down backend-build backend-run

dev-up:
	$(COMPOSE) up -d

dev-build:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

logs-backend:
	$(COMPOSE) logs -f backend

logs-frontend:
	$(COMPOSE) logs -f frontend

logs-db:
	$(COMPOSE) logs -f db

ps:
	$(COMPOSE) ps

clean:
	$(COMPOSE) down -v --remove-orphans

migrate-up:
	$(COMPOSE) exec backend migrate -path /app/database/migrations -database "postgres://$$POSTGRES_USER:$$POSTGRES_PASSWORD@db:5432/$$POSTGRES_DB?sslmode=disable" up

migrate-down:
	$(COMPOSE) exec backend migrate -path /app/database/migrations -database "postgres://$$POSTGRES_USER:$$POSTGRES_PASSWORD@db:5432/$$POSTGRES_DB?sslmode=disable" down

# Run from repository root (kaima/) so process cwd stays the repo root (e.g. .env next to Makefile).
backend-build:
	go build -C $(BACKEND_DIR) ./...

backend-run: backend-build
	mkdir -p $(dir $(BACKEND_BIN))
	go build -C $(BACKEND_DIR) -o $(BACKEND_BIN) ./cmd
	$(BACKEND_BIN)

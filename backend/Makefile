.PHONY: up down gen migrate seed lint test e2e run-api build tools

INFRA_COMPOSE := ../infra/docker-compose.yml
TOOLS_DIR := ../tools
BIN_DIR := bin

# Читаются из ENV, если заданы (см. .env.example); значения по умолчанию совпадают с infra/docker-compose.yml.
# migrate запускается в контейнере на сети inspector-net, поэтому ходит в postgres по имени сервиса,
# а не через localhost (см. цель migrate).
DATABASE_USER ?= postgres
DATABASE_PASSWORD ?= postgres
DATABASE_DATABASE ?= inspector
MIGRATE_IMAGE := migrate/migrate:v4.18.1

up: ## Поднять всю инфраструктуру (postgres, minio, kafka, ...)
	docker compose -f $(INFRA_COMPOSE) up -d

down: ## Остановить и удалить инфраструктуру
	docker compose -f $(INFRA_COMPOSE) down

# oapi-codegen и golangci-lint собираются из отдельного модуля ../tools (свой go.mod), а не через
# require backend/go.mod — иначе go mod tidy подтягивает go-версию backend'а под требования линтера
# (golangci-lint/v2 всегда требует "latest Go minus one"), а backend/CLAUDE.md фиксирует Go 1.24.
$(BIN_DIR)/oapi-codegen:
	@mkdir -p $(BIN_DIR)
	go build -C $(TOOLS_DIR) -o $(CURDIR)/$(BIN_DIR)/oapi-codegen github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen

$(BIN_DIR)/golangci-lint:
	@mkdir -p $(BIN_DIR)
	go build -C $(TOOLS_DIR) -o $(CURDIR)/$(BIN_DIR)/golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint

tools: $(BIN_DIR)/oapi-codegen $(BIN_DIR)/golangci-lint ## Собрать dev-инструменты в bin/

gen: $(BIN_DIR)/oapi-codegen ## Сгенерировать код из contracts/openapi.yaml
	./$(BIN_DIR)/oapi-codegen -config oapi-codegen.yaml ../contracts/openapi.yaml

migrate: ## Применить миграции к базе (migrations/) через golang-migrate
	@if [ -z "$(wildcard migrations/*.sql)" ]; then \
		echo "migrate: миграций пока нет (появятся в M1, backend-plan.md §5), пропускаю"; \
	else \
		docker run --rm --network inspector-net \
			-v $(CURDIR)/migrations:/migrations \
			$(MIGRATE_IMAGE) \
			-path=/migrations -database "postgres://$(DATABASE_USER):$(DATABASE_PASSWORD)@postgres:5432/$(DATABASE_DATABASE)?sslmode=disable" \
			up; \
	fi

seed: ## Импорт Матрицы параметров + тестовые пользователи
	go run ./cmd/tools/import-matrix
	go run ./cmd/tools/seed-users

lint: $(BIN_DIR)/golangci-lint ## golangci-lint
	./$(BIN_DIR)/golangci-lint run ./...

test: ## Unit + интеграционные тесты (testcontainers поднимает свои контейнеры сам)
	go test ./...

e2e: ## Сквозной сценарий на тестовом объекте
	@echo "e2e: TODO(M5), см. backend-plan.md" && exit 1

build: ## Собрать все бинари cmd/*
	go build ./...

run-api: ## Запустить API локально (ожидает `make up` и `make migrate`)
	go run ./cmd/api

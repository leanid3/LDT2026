# Локальная разработка

Как поднять стенд на своей машине, собрать и запустить сервис, сгенерировать код из контрактов,
прогнать тесты и линтер. Для развёртывания на общем демо-стенде — [deployment.md](deployment.md).
Для эксплуатационных вопросов (мониторинг, бэкапы, инциденты) — [operations.md](operations.md).

## Требования

| Инструмент | Версия | Зачем |
|---|---|---|
| Go | 1.24.x (или новее — `GOTOOLCHAIN=auto` сам подтянет нужный) | сборка `backend/` |
| Docker + Docker Compose v2 | любая современная | `infra/docker-compose.yml` |
| gcc/g++ | любая | `CGO_ENABLED=1` для `confluent-kafka-go/v2` |
| make | любая | `backend/Makefile` |

`confluent-kafka-go/v2` линкуется статически со встроенной `librdkafka` — системный `librdkafka-dev`
**не нужен**, но `cgo` всё равно требует `gcc`/`g++`.

## Быстрый старт

```bash
cd backend
cp config.example.yaml config.yaml   # не коммитится, см. .gitignore
make up                              # postgres, kafka, minio, redis, clamav, gotenberg,
                                      # prometheus, grafana, alertmanager, kafka-ui, caddy
make migrate                         # применить миграции (0001-0007, черновая схема §5)
make run-api                         # go run ./cmd/api, слушает :8080 (host, не в контейнере)
```

Проверка:

```bash
curl -s localhost:8080/healthz | jq        # {"status":"ok"}
curl -s localhost:8080/readyz  | jq        # {"status":"ready"} — реальный ping в postgres
curl -s localhost:9091/metrics | grep inspector_
open http://localhost:8090                 # kafka-ui — список топиков
open http://localhost:8080/swagger         # Swagger UI по contracts/openapi.yaml
```

Почему API запускается на хосте, а не в docker-compose: на текущем этапе (M0) это проще для итеративной
разработки — `go run` вместо пересборки образа на каждое изменение. `infra/docker-compose.yml` и Caddy
уже настроены на `host.docker.internal:8080`, поэтому реверс-прокси и Prometheus видят локально
запущенный процесс. Контейнеризация `api`/`relay`/`engine` для стенда — см. [deployment.md](deployment.md).

## Конфигурация

`internal/config.Load` читает `config.yaml`, затем переопределяет значения из ENV (`cleanenv`); если
файла нет — конфигурация целиком из ENV. Секреты (пароли, ключи) — **только через ENV**, в
`config.yaml`/`.env` в репозитории быть не должно (см. `.gitignore`, правило №10 в
[backend/CLAUDE.md](../backend/CLAUDE.md)). Актуальные поля и дефолты — исходный источник истины:
[backend/internal/config/config.go](../backend/internal/config/config.go); примеры значений —
[backend/config.example.yaml](../backend/config.example.yaml) и [backend/.env.example](../backend/.env.example).

Порты по умолчанию (см. `infra/docker-compose.yml` и `config.example.yaml`):

| Сервис | Порт (хост) |
|---|---|
| API (`make run-api`) | 8080 |
| Metrics | 9091 |
| PostgreSQL | 5432 |
| MinIO API / консоль | 9000 / 9001 |
| Kafka (внешний listener) | 9092 |
| kafka-ui | 8090 |
| Redis | 6379 |
| ClamAV | 3310 |
| Gotenberg | 3000 |
| Prometheus | 9090 |
| Grafana | 3001 |
| Alertmanager | 9093 |
| Caddy (https / http) | 8443 / 8081 |

## Команды `make` (`backend/Makefile`)

| Команда | Что делает |
|---|---|
| `make up` / `make down` | Поднять/остановить `infra/docker-compose.yml` |
| `make tools` | Собрать dev-инструменты (`oapi-codegen`, `golangci-lint`) в `backend/bin/` из модуля `tools/` |
| `make gen` | Сгенерировать `internal/transport/http/gen` из `contracts/openapi.yaml` |
| `make migrate` | Применить миграции `migrations/*.sql` через `golang-migrate` (Docker-образ, сеть `inspector-net`) |
| `make seed` | `cmd/tools/import-matrix` + `cmd/tools/seed-users` (доступно с M2) |
| `make lint` | `golangci-lint run ./...` |
| `make test` | `go test ./...` (интеграционные тесты сами поднимают testcontainers) |
| `make e2e` | Сквозной сценарий на тестовом объекте (доступно с M5) |
| `make build` | `go build ./...` — все бинари `cmd/*` |
| `make run-api` | `go run ./cmd/api` |

`make gen` и `make lint` при первом запуске сами соберут нужные бинари из `tools/` (может занять время
на первый прогон — скачивание/компиляция golangci-lint) и закешируют их в `backend/bin/` (в `.gitignore`,
не коммитится). Почему это отдельный модуль, а не `go tool` в `backend/go.mod` —
[architecture.md#tools-модуль](architecture.md#tools-модуль-и-почему-он-отдельный).

## Миграции (`backend/migrations/`)

> **Черновик.** `0001_platform` … `0007_ml` реализуют схему из [backend-plan.md §5](backend-plan.md#5-схема-бд-миграции)
> заранее, до согласования финальной модели данных с остальной командой — чтобы можно было проверять
> API/домен локально не дожидаясь финальной схемы. Типы и индексы в них предварительные и подлежат
> уточнению; смысл полей и статусов менять нельзя (backend-plan.md §4.1). Когда придёт согласованная
> схема — правки вносятся **новыми** миграциями поверх этих (правило №2, `backend/CLAUDE.md`: уже
> применённая миграция не редактируется), а не переписыванием существующих файлов.

```bash
cd backend
make migrate                         # применяет все *.up.sql по порядку, идемпотентно ("no change" повторно)
docker compose -f ../infra/docker-compose.yml exec postgres psql -U postgres -d inspector -c '\dt'
```

Откат при необходимости (например, локально проверить `down`-миграции или пересобрать БД с нуля):

```bash
docker run --rm --network inspector-net \
    -v "$(pwd)/migrations:/migrations" \
    migrate/migrate:v4.18.1 \
    -path=/migrations -database "postgres://postgres:postgres@postgres:5432/inspector?sslmode=disable" \
    down -all
```

## Изменение API-контракта

Правило работы №1 (`backend/CLAUDE.md`): контракт первым.

1. Правки в [contracts/openapi.yaml](../contracts/openapi.yaml).
2. `cd backend && make gen` — перегенерировать `internal/transport/http/gen`.
3. Реализовать/поправить хендлер.
4. Обновить `docs/backend-plan.md §7`, если меняется состав эндпоинтов.

Аналогично для событий Kafka — сначала JSON Schema в `contracts/events/`, `contracts/*.schema.json`.

## Тесты

```bash
cd backend
make test          # go test ./...
go test ./... -short   # быстрый прогон (пропускает testcontainers-тесты, как в CI)
```

Интеграционные тесты используют `testcontainers-go` (модули postgres/kafka/minio) и поднимают свои
контейнеры — не нужно заранее делать `make up`. Юнит-тесты (`internal/platform/logging`,
`internal/config`) используют `testify`.

## Типичные проблемы

| Симптом | Причина / решение |
|---|---|
| `make migrate` падает на уже применённой миграции с изменённым содержимым | Миграции не редактируются задним числом (правило №2) — накатите новую миграцию вместо правки старой |
| `ERROR: relation "..." already exists` при `make migrate` | БД в состоянии `dirty` после прерванной миграции — проверьте `SELECT * FROM schema_migrations`, при необходимости `migrate ... force <версия>` перед повтором |
| `go build` тянет новый Go toolchain при первом запуске `make lint`/`make gen` | Нормально — `tools/go.mod` требует более новый Go, `GOTOOLCHAIN=auto` скачивает его автоматически только для сборки `tools/`, `backend/go.mod` остаётся на 1.24 |
| Kafka healthcheck долго не проходит | Первый старт KRaft форматирует storage directory, подождите — `retries: 30`, интервал 5с в compose |
| ClamAV долго `starting` | Первый запуск качает базы сигнатур, `start_period: 120s` в healthcheck — это ожидаемо |
| Порт 8080 занят | Проверьте, не запущен ли уже `cmd/api` (`pkill -f cmd/api`) или другой процесс на этом порту |
| `docker compose up` падает на образе Kafka/ClamAV с «not found» | Тег образа в `infra/docker-compose.yml` устарел на Docker Hub — проверить актуальные теги и обновить (уже случалось с `bitnami/kafka` и `clamav/clamav:1.3`, см. git-историю `infra/docker-compose.yml`) |

Остановить стенд после работы: `make down` (данные в именованных volume сохраняются, `docker compose down -v`
удалит их безвозвратно — не делайте это не глядя).

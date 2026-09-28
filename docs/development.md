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
make migrate                         # применить миграции (0001-0009, черновая схема §5)
make seed                            # тестовые пользователи (inspector1/supervisor1/admin1/ml1,
                                      # пароль demo-password-123 — см. cmd/tools/seed-users)
make run-api                         # go run ./cmd/api, слушает :8080 (host, не в контейнере)
```

Проверка:

```bash
curl -s localhost:8080/healthz | jq        # {"status":"ok"}
curl -s localhost:8080/readyz  | jq        # {"status":"ready"} — реальный ping в postgres/minio/clamav
curl -s localhost:9091/metrics | grep inspector_
open http://localhost:8090                 # kafka-ui — список топиков
open http://localhost:8080/swagger         # Swagger UI по contracts/openapi.yaml
```

Для полного сквозного прогона (загрузка → проверка → протокол → верификация → финализация →
синхронизация) нужны ещё пять процессов — см.
[«Полный конвейер локально»](#полный-конвейер-локально) ниже.

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

## Полный конвейер локально

`make run-api` одного достаточно только для auth/objects и осмотра уже готовых данных. Чтобы реально
прогнать документ через весь конвейер (upload → confirm → registry → start → парсинг/извлечение →
движок проверок → протокол → верификация → финализация → синхронизация с ИАИС «РиН»), нужны ещё
процессы — каждый в своём терминале (или фоном). Перед этим: `make up migrate seed` (`seed` грузит
132 параметра Матрицы и тестовых пользователей).

```bash
# backend/
make run-relay        # outbox_events -> Kafka
make run-engine       # fan-in/out + валидация фактов + движок проверок (backend/rules, 71 правило)
make run-rin-mock     # заглушка ИАИС «РиН», слушает :8082 по умолчанию
RIN_ENDPOINT=http://localhost:8082 make run-rin-sync

# workers/ — настоящие воркеры (нужен один раз: make venv)
make run-parse        # doc.parse.requested  -> layout   -> doc.parse.completed
make run-extract      # doc.extract.requested -> факты    -> doc.extract.completed
```

> Вместо `workers/` можно запустить Go-заглушку `make run-mockworkers` (backend/) — она отвечает
> синтетикой без разбора файлов. **Только одно из двух**: обе пары слушают одни топики и ответили бы
> дважды.

**Один скрипт вместо ручных curl** — `scripts/e2e.py` (только stdlib) проходит весь путь на двух
PDF-фикстурах (ПД и РД с заложенными расхождениями), проверяет карточки с доказательствами,
скачивание файла, каталог параметров, CORS и `SYNCED` в конце:

```bash
python3 scripts/e2e.py          # или: cd backend && make e2e
```

Вручную через `/swagger` или `curl`: логин → `POST /objects` → `POST /documents/upload` (presigned POST
в MinIO) → реальный POST файла по `upload_url` → `POST /documents/{process_id}/confirm` → `POST
/documents/{process_id}/registry` (CSV/JSON/XLSX; **без реестра у файлов нет стадии ПД/РД/ИД и извлечение
не даст фактов**) → `POST /processes/{id}/start` → `GET /processes/{id}` до `READY` → `GET
/processes/{id}/protocol` → `POST /findings/{id}/decision` по `CANDIDATE` → `POST
/processes/{id}/finalize` → `GET /processes/{id}/sync` до `SYNCED`.

Настоящие воркеры — [workers/README.md](../workers/README.md). Не готово (внешнее или отложено):
[architecture.md#границы-текущей-реализации](architecture.md#границы-текущей-реализации).

## Команды `make` (`backend/Makefile`)

| Команда | Что делает |
|---|---|
| `make up` / `make down` | Поднять/остановить `infra/docker-compose.yml` |
| `make tools` | Собрать dev-инструменты (`oapi-codegen`, `golangci-lint`) в `backend/bin/` из модуля `tools/` |
| `make gen` | Сгенерировать `internal/transport/http/gen` из `contracts/openapi.yaml` |
| `make migrate` | Применить миграции `migrations/*.sql` через `golang-migrate` (Docker-образ, сеть `inspector-net`) |
| `make seed` | `cmd/tools/import-matrix` (132 параметра из `docs/source/*.xlsx` → `params` и `contracts/matrix.json`) + `cmd/tools/seed-users` |
| `make lint` | `golangci-lint run ./...` |
| `make test` | `go test ./...` (интеграционные тесты сами поднимают testcontainers) |
| `make e2e` | `scripts/e2e.py` — сквозной сценарий через публичный API (нужны стенд, сервисы и воркеры) |
| `make build` | `go build ./...` — все бинари `cmd/*` |
| `make run-api` | `go run ./cmd/api` |
| `make run-relay` / `run-engine` / `run-mockworkers` / `run-rin-sync` / `run-rin-mock` | Остальные сервисы конвейера — см. [«Полный конвейер локально»](#полный-конвейер-локально) |

`make gen` и `make lint` при первом запуске сами соберут нужные бинари из `tools/` (может занять время
на первый прогон — скачивание/компиляция golangci-lint) и закешируют их в `backend/bin/` (в `.gitignore`,
не коммитится). Почему это отдельный модуль, а не `go tool` в `backend/go.mod` —
[architecture.md#tools-модуль](architecture.md#tools-модуль-и-почему-он-отдельный).

## Миграции (`backend/migrations/`)

> **Черновик.** `0001_platform` … `0009_facts_sha256_nullable` реализуют схему из [backend-plan.md §5](backend-plan.md#5-схема-бд-миграции)
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

Python-воркеры: `cd workers && make test` (93 теста; интеграционные с Postgres включаются
`TEST_DATABASE_DSN="host=localhost port=5432 user=postgres password=postgres dbname=inspector"`).

Интеграционные тесты используют `testcontainers-go` (модули `postgres` и `minio` — `internal/platform/dbtest`)
и поднимают свои контейнеры — не нужно заранее делать `make up`, тесты полностью изолированы от
локального стенда. Ими покрыты почти все домены (`auth`, `audit`, `objects`, `files` — включая
полный upload→confirm через реальный MinIO, `process`, `findings`, `protocol`, `engine`, `mockworkers`,
`rin`); Kafka-testcontainers не используется — consumer-логика тестируется напрямую вызовом
`Handler.Handle(ctx, msg)` на сконструированном сообщении, без реального брокера. Чистые функции
(`files.SelectCurrent`, `process.ComputeScenario`, `engine/rules.Evaluate`) покрыты обычными
табличными юнит-тестами без БД.

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
| `doc.parse.requested`/другие события публикуются (`outbox_events.status='published'`), но ни один consumer (`cmd/mockworkers`/`cmd/engine`/`cmd/rin-sync`) их не забирает; в `docker logs infra-kafka-1` — `Auto topic creation failed for __consumer_offsets with error 'INVALID_REPLICATION_FACTOR'` | Single-node KRaft: дефолтный replication factor внутренних топиков (`__consumer_offsets`, `__transaction_state`) — 3, а брокер один. Уже исправлено в `infra/docker-compose.yml` (`KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1` и т.п.) — если ловите эту ошибку, у вас старый образ: `docker compose stop kafka && docker compose rm -f kafka && docker volume rm infra_kafkadata && docker compose up -d kafka kafka-init` (данные Kafka не бизнес-критичны, Postgres не трогается) |
| `docker pull minio/minio` → «pull access denied» / `make up` не поднимает MinIO | `minio/minio` и `minio/mc` удалены с Docker Hub. Стенд и тесты используют `cgr.dev/chainguard/minio` (см. [architecture.md#хранилище-файлов-minio](architecture.md#хранилище-файлов-minio)); `docker login` тут не поможет |
| В логе engine: `message moved to DLQ` | Сообщение не обработано за 3 попытки и отправлено в `<topic>.dlq` (причина — в заголовке `dlq_error`). Чаще всего — устаревшие события в Kafka после сброса БД. Посмотреть: `kafka-console-consumer.sh --topic doc.parse.completed.dlq --from-beginning --property print.headers=true` |
| Процесс завис в `PARSING`, ошибок нет | Не запущен один из звеньев: `relay`, воркер (`run-parse`/`run-extract` или `run-mockworkers`), `engine`. Проверьте `outbox_events` (`status`), лаг consumer group в kafka-ui (:8090). Таймаутов заданий пока нет — см. границы |
| `relay` в логах: `Failed to acquire idempotence PID from broker ...: Coordinator load in progress: retrying` сразу после старта Kafka | Транзакционный координатор ещё поднимается после чистого старта — проходит само за несколько секунд, не признак поломки |

Остановить стенд после работы: `make down` (данные в именованных volume сохраняются, `docker compose down -v`
удалит их безвозвратно — не делайте это не глядя).

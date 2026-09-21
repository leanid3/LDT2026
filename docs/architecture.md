# Архитектура

Этот документ описывает, как устроен backend «Инспектор ИИ»: из чего он состоит, какими принципами и
паттернами связаны части, и почему приняты ключевые технические решения. Модель данных, контракты
событий и полный список требований ТЗ — в [backend-plan.md](backend-plan.md); здесь — только то, что
нужно, чтобы ориентироваться в коде и не наступать на грабли, которые уже были учтены при проектировании.

## Обзор

```mermaid
flowchart LR
    Inspector["Инспектор /\nадминистратор"] -->|HTTPS| Caddy
    Caddy -->|"reverse proxy :8080"| API["cmd/api"]

    API -->|"WithTx + outbox_events"| PG[(PostgreSQL)]
    API -->|"presigned POST/GET"| Minio[(MinIO)]

    Relay["cmd/relay"] -->|"SELECT ... FOR UPDATE SKIP LOCKED"| PG
    Relay -->|publish| Kafka{{Kafka}}

    Kafka -->|doc.parse.requested| ParseWorker["parse-воркер (Python)"]
    ParseWorker -->|doc.parse.completed| Kafka
    Kafka -->|doc.extract.requested| LLMWorker["LLM-воркер (Python)"]
    LLMWorker -->|doc.extract.completed| Kafka

    Kafka -->|"consume + WithTx"| Engine["cmd/engine"]
    Engine --> PG
    Engine -->|outbox| Relay

    Kafka -->|rin.sync.requested| RinSync["cmd/rin-sync"]
    RinSync -->|УКЭП / HTTP| RIN["ИАИС «РиН»"]

    Frontend["frontend (React)"] -->|HTTPS| Caddy
```

Ключевая идея: **всё тяжёлое — асинхронно** (ТЗ 11, p95 API ≤ 200 мс). `cmd/api` только принимает
запросы, читает/пишет Postgres и отдаёт быстрый ответ; разбор документов, сравнение параметров и
доставка во внешние системы происходят в фоновых сервисах, связанных через Kafka.

## Компоненты (`backend/cmd/*`)

Один Docker-образ, один бинарь на команду `cmd/`; конкретный сервис в docker-compose/kubernetes
выбирается командой запуска (`/app/bin/api`, `/app/bin/engine`, ...) — см. [backend/Dockerfile](../backend/Dockerfile).

| Бинарь | Роль | Веха |
|---|---|---|
| `cmd/api` | HTTP API: приём файлов, процессы, верификация, админка, `/healthz` `/readyz` `/metrics` | M0 (каркас), домен — M1+ |
| `cmd/relay` | Единственный писатель в Kafka: забирает `outbox_events` и публикует | M1 |
| `cmd/engine` | Consumer результатов воркеров, движок проверок по 132 параметрам, контроль таймаутов заданий | M2–M3 |
| `cmd/rin-sync` | Отправка финализированных протоколов в ИАИС «РиН» с ретраями (1/5/15 мин) | M4 |
| `cmd/rin-mock` | Заглушка ИАИС «РиН» для демо и тестов ретраев | M4 |
| `cmd/tools/import-matrix` | Импорт Матрицы параметров (132 строки) из xlsx в `params` | M2 |
| `cmd/tools/seed-users` | Тестовые пользователи для демо | M2 |
| `cmd/tools/export-dataset` | Экспорт GOLD-датасета для дообучения модели | M5 |

## `internal/platform` — общая инфраструктура

Пакеты, от которых зависят все `cmd/*`, но которые ничего не знают о домене (объекты, процессы,
findings). Домен в `internal/<domain>/` появляется начиная с M1 и строится поверх этого слоя.

| Пакет | Отвечает за |
|---|---|
| `internal/config` | Загрузка конфигурации: `config.yaml` + переопределение из ENV (`cleanenv`); без файла — только ENV |
| `internal/platform/db` | Пул `pgx/v5`, health-check, `WithTx` — unit-of-work для транзакций |
| `internal/platform/kafka` | `Producer`/`Consumer` поверх `confluent-kafka-go/v2`: delivery reports, ручной commit offset |
| `internal/platform/minio` | Клиент MinIO, обеспечение бакета |
| `internal/platform/httpx` | Обёртка gin-сервера (graceful shutdown) + `AppError`/`ErrorResponse` (единый формат ошибок) |
| `internal/platform/httpx/middleware` | `RequestID`, `Recovery`, `Logger` |
| `internal/platform/logging` | `slog.Logger`, JSON в stdout, поля `timestamp/level/service/message/...` |
| `internal/platform/metrics` | `prometheus/client_golang`, namespace `inspector_`, middleware для HTTP-метрик |
| `internal/platform/health` | `/healthz` (liveness) и `/readyz` (readiness, гоняет `Checker`-и по зависимостям) |

## Паттерны

### Spec-first контракт (правило работы №1, `backend/CLAUDE.md`)

`contracts/openapi.yaml` — источник истины для HTTP API. Любое изменение API: сначала правки в
`openapi.yaml`, затем `make gen` (генерирует `internal/transport/http/gen` через `oapi-codegen`,
профиль `gin-server` + `types`), затем реализация хендлера. Свой Swagger UI сервис отдаёт статикой
по `/swagger` (см. `registerSwagger` в [cmd/api/main.go](../backend/cmd/api/main.go)) — без swaggo/swag
(генерирует устаревший Swagger 2.0, был в исходном каркасе, убран при переносе).

То же правило для событий Kafka: сначала JSON Schema в `contracts/events/`, `contracts/facts.schema.json`,
`contracts/layout.schema.json` — они общий контракт с Python-воркерами, менять без предупреждения нельзя.

### Unit of work (`db.WithTx`)

```go
err := database.WithTx(ctx, func(tx pgx.Tx) error {
    // бизнес-изменение и outbox-запись — одна транзакция
    if err := saveDomainChange(ctx, tx, ...); err != nil {
        return err
    }
    return insertOutboxEvent(ctx, tx, ...)
})
```

`WithTx` в [internal/platform/db/db.go](../backend/internal/platform/db/db.go) коммитит при успехе,
откатывает при ошибке или панике. Это основа паттерна Transactional Outbox ниже — без единой транзакции
на бизнес-изменение и запись в outbox между Postgres и Kafka возможна рассинхронизация (записали в БД,
не отправили событие, или наоборот).

### Transactional Outbox + Relay

Бизнес-код **никогда не пишет в Kafka напрямую** — только `cmd/relay` (правило работы №5). Вместо этого
в той же транзакции, что бизнес-изменение, делается `INSERT INTO outbox_events`. `cmd/relay`:

1. Забирает пачку `SELECT ... FOR UPDATE SKIP LOCKED LIMIT 100`, ставит `status='publishing'`.
2. Публикует в Kafka, дожидается delivery report → `status='published'`.
3. Ошибка → `status='failed'`, `available_at` с экспоненциальным backoff (до 60 с). Несколько реплик
   relay безопасны за счёт `SKIP LOCKED`; зависшие `publishing` дольше 2 минут переподхватываются.

Гарантия: если бизнес-транзакция закоммитилась — событие рано или поздно будет опубликовано (at-least-once).
Consumers поэтому обязаны быть идемпотентными (см. ниже).

### Идемпотентный consumer

`enable.auto.commit=false` жёстко зашито в `internal/platform/kafka` (не настраивается конфигом —
это была одна из обязательных правок при переносе старого каркаса, см. backend-plan.md §2). Цикл
обработки:

```
BEGIN
  INSERT INTO consumed_events (consumer_name, event_id) ON CONFLICT DO NOTHING
  -- 0 строк вставлено → уже обработано, просто commit offset
  бизнес-логика
  INSERT INTO outbox_events (...)  -- если нужно
COMMIT
commit Kafka offset
```

Offset коммитится только после успешного коммита транзакции Postgres — иначе при падении между
`COMMIT` и коммитом offset сообщение переобработается, но `consumed_events` не даст применить его дважды.
После 3 неудачных попыток с backoff — публикация в `<topic>.dlq` и commit offset (не блокировать партицию).

### Единый формат ошибок API

`internal/platform/httpx/errors.go`: `AppError{Code, Message, HTTPStatus, Details}` →
`ErrorResponse(err, requestID)` сериализует в `{"error": {"code","message","details"}, "request_id"}`
(backend-plan.md §7). `RequestID` прокидывается через `middleware.RequestID()` в заголовок ответа и в
логи каждого запроса — по нему можно сопоставить лог, ответ клиенту и запись в `audit_log`.

### Один образ — несколько бинарей

`backend/Dockerfile` собирает `api/relay/engine/rin-sync/rin-mock` в один слой `/app/bin/`; какой
бинарь запускать, решает `command:` в docker-compose/манифесте. Это упрощает сборку и версионирование
(один тег образа = одна версия всех сервисов), а не про экономию места.

### Graceful shutdown

`cmd/api/main.go`: `signal.NotifyContext(SIGINT, SIGTERM)` → `select` между сигналом и ошибкой сервера →
`server.Shutdown()` с таймаутом (`ShutdownTimeout`, по умолчанию 5с) → закрытие пула БД. Тот же принцип
для будущих `relay`/`engine`: `consumer.Close()` и `producer.Flush()` перед выходом (проверено в
исходном каркасе при переносе, см. backend-plan.md §2 «Проверить в перенесённом коде»).

## `tools/`-модуль и почему он отдельный

`backend/go.mod` жёстко пин Go 1.24 (`backend/CLAUDE.md`: «не менять без согласования с человеком»).
Но `golangci-lint/v2` по своей политике всегда требует «последнюю версию Go минус одна» — т.е. любая
версия v2.x как прямая зависимость `backend/go.mod` рано или поздно заставит `go mod tidy` поднять
`go`-директиву. Такая же проблема была с `embedded-spec: true` в `oapi-codegen.yaml`: генератор тянет
`kin-openapi`, чьи транзитивные зависимости тоже требуют более новый Go (сейчас `embedded-spec`
отключён — Swagger UI и так читает `contracts/openapi.yaml` напрямую, эмбеддить спеку в бинарь незачем).

Решение: `tools/` — независимый Go-модуль (свой `go.mod`, версия Go не связана с `backend/`), в котором
живут `tool`-директивы на `golangci-lint/v2` и `oapi-codegen/v2`. `backend/Makefile` собирает из него
обычные бинари в `backend/bin/` (`go build -C ../tools -o ...`, см. цели `gen`/`lint`/`tools`) и
дальше вызывает их как любой внешний CLI. Это полностью развязывает версию Go в `backend/go.mod` от
требований dev-инструментов. `GOTOOLCHAIN=auto` (по умолчанию) сам скачивает нужный toolchain при сборке
`tools/`, вручную ничего ставить не нужно.

Если добавляете новый dev-инструмент через `go get -tool` — делайте это в `tools/go.mod`, не в
`backend/go.mod`.

## Данные

Полная схема миграций — [backend-plan.md §5](backend-plan.md#5-схема-бд-миграции). Коротко, по
слоям (каждый — отдельная миграция, миграции только добавляются, не редактируются — правило №2):

- `0001_platform` — `users`, `outbox_events`, `consumed_events`, `idempotency_keys`, `audit_log`.
- `0002_objects_files` — `objects`, `processes`, `files`, `registry_entries`, `upload_status`.
- `0003_parsing` — `parse_jobs`, `extract_jobs`.
- `0004_matrix` — `params` (132 параметра), `normative_base`, `logical_rules`.
- `0005_checks` — `facts`, `evidence_groups`, `checks`, `evidence_fragments`, `findings`, `suspicions`.
- `0006_protocol` — `protocols`, `rejection_log`, `dispute_log`.
- `0007_ml` — `dataset_items`, `model_versions`, `ml_retraining_log`.

## Событийная архитектура

Конверт события (все топики):

```json
{
  "event_id": "uuid",
  "event_type": "doc.parse.requested",
  "schema_version": 1,
  "occurred_at": "RFC3339",
  "correlation_id": "process_id",
  "object_id": "uuid",
  "process_id": "uuid",
  "data": { "...": "только ссылки на MinIO, не содержимое — лимит сообщения ~1 МБ" }
}
```

Ключ сообщения — `object_id` (события одного объекта попадают в одну партицию — порядок сохраняется
внутри объекта). Топики (3 партиции / 7 дней для рабочих, 1 партиция / 30 дней для `*.dlq`):

| Топик | Producer | Consumer |
|---|---|---|
| `doc.parse.requested` | relay (из api) | parse-воркер |
| `doc.parse.completed` | parse-воркер | engine |
| `doc.extract.requested` | relay (из engine) | LLM-воркер |
| `doc.extract.completed` | LLM-воркер | engine |
| `protocol.ready` | relay (из engine) | уведомления (опционально) |
| `rin.sync.requested` | relay (из api, при финализации) | rin-sync |
| `*.dlq` | любой consumer после исчерпания попыток | алерт, ручной replay |

## Архитектурные решения

Осознанные отступления от ТЗ (ТЗ по умолчанию предполагал Node.js/RabbitMQ):

- **Go вместо Node.js.** Компилируемый бинарь без рантайма, сильная типизация для доменной модели
  со строгими enum'ами (раздел 4.1 ТЗ), зрелые библиотеки для pgx/Kafka/MinIO, конкурентность
  (`goroutine`) естественно ложится на fan-out/fan-in заданий парсинга (`parse_jobs`/`extract_jobs`).
- **Kafka вместо RabbitMQ.** Нужен durable event log с воспроизводимостью (переиграть `doc.extract.completed`
  при обновлении правил без повторного парсинга), партиционирование по `object_id` для упорядоченности,
  и консьюмер-группы для нескольких инстансов `cmd/engine`. У RabbitMQ это сложнее и менее естественно.
- **Kafka в режиме KRaft** (`apache/kafka`, без ZooKeeper) — меньше движущихся частей на демо-стенде,
  ZooKeeper для одного брокера не даёт выгоды.
- **Отдельный `tools/`-модуль** — см. раздел выше.

Обе замены осознанные и зафиксированы здесь по требованию ТЗ (задокументировать отступления).

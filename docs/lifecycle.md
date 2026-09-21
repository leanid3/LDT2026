# Жизненные циклы

Архитектура и паттерны — в [architecture.md](architecture.md). Здесь — как во времени живут ключевые
сущности и процессы: от старта сервиса до финализации протокола проверки. Enum-значения — канонические,
строго как в [backend-plan.md §4.1](backend-plan.md#41-enum-значения-строго-как-в-тз) (правило работы №3:
не придумывать новые названия статусов).

## Процесс проверки (`process_status`)

Центральный жизненный цикл домена — один процесс проверки одного объекта.

```mermaid
stateDiagram-v2
    [*] --> PENDING: создание процесса (загрузка/дозагрузка файлов)
    PENDING --> PARSING: POST /processes/{id}/start
    PARSING --> READY: все parse/extract-задания завершены,\nпротокол собран
    READY --> VERIFYING: первое решение инспектора\n(POST /findings/{id}/decision)
    VERIFYING --> COMPLETED: нет CANDIDATE с\ninspector_status=PENDING
    COMPLETED --> FINALIZED: POST /processes/{id}/finalize
    FINALIZED --> COMPLETED: POST /processes/{id}/unfinalize\n(supervisor/admin, обязательна причина, аудит)

    READY --> PARSING: дозагрузка файлов (инкрементально)
    VERIFYING --> PARSING: дозагрузка файлов (инкрементально)
    COMPLETED --> PARSING: дозагрузка файлов (инкрементально)

    note right of PARSING
      Дозагрузка в PARSING: 409 + Retry-After
      Дозагрузка в FINALIZED: 409
    end note
```

Правила переходов (backend-plan.md §4.2, обязательны к соблюдению в коде домена `process`, начиная с M2):

- **Дозагрузка файлов** разрешена в `PENDING`, `READY`, `VERIFYING`, `COMPLETED`. В `PARSING` — `409` с
  `Retry-After`. В `FINALIZED` — `409` (после финализации дозагрузка только помечает «есть новые
  документы» и предлагает создать новый процесс, автоматической проверки не запускает).
- **Верификация findings** разрешена в `READY`, `VERIFYING`, `COMPLETED` (до финализации).
- **Финализация** — только если у всех `findings` со статусом `CANDIDATE` `inspector_status != PENDING`
  (допускается перевод в `CLARIFICATION_REQUIRED`). `MISSING_EVIDENCE` выводится отдельным перечнем,
  не блокирует финализацию.
- **`unfinalize`** — только роли `supervisor`/`admin`, обязательное поле `reason`, событие в `audit_log`.
- Финализация публикует `rin.sync.requested` (outbox) и переводит `protocols.sync_status` в `PENDING_SYNC`.

## Файл (`file_check_status`)

```mermaid
stateDiagram-v2
    [*] --> UPLOADING: POST /documents/upload\n(presigned POST выдан)
    UPLOADING --> UPLOADED: файл записан в MinIO
    UPLOADED --> ACCEPTED: confirm — sha256 + ClamAV\n+ magic bytes + pdfcpu пройдены
    UPLOADED --> REJECTED_FORMAT
    UPLOADED --> REJECTED_SIZE
    UPLOADED --> REJECTED_CORRUPTED
    UPLOADED --> REJECTED_VIRUS: объект удаляется из MinIO
```

Дубликат по `sha256` в рамках процесса не создаёт новую запись — возвращается существующий `file_id`
(backend-plan.md §8.1). `ACCEPTED`-файлы дальше проходят через выбор актуальной редакции
(`selection_status`: `is_current` / `CLARIFICATION_REQUIRED` с причиной, backend-plan.md §8.2) —
это отдельный слой поверх `file_check_status`, не заменяет его.

## Finding

Два независимых поля: `finding_status` (что решил движок проверок) и `inspector_status` (что решил
человек).

```mermaid
stateDiagram-v2
    state finding_status {
        [*] --> CANDIDATE: расхождение найдено
        [*] --> NEGATIVE_VERIFIED: расхождения нет
        [*] --> MISSING_EVIDENCE: обязательный источник отсутствует
        [*] --> NOT_APPLICABLE: параметр/стадия неприменимы
        [*] --> NOT_COMPARABLE: нет правила или нет фактов нужного качества
        [*] --> CLARIFICATION_REQUIRED: источник из группы с неоднозначной редакцией
        [*] --> SUSPICION: свободный поиск (не нарушение)
    }
    state inspector_status {
        [*] --> PENDING
        PENDING --> CONFIRMED_VIOLATION: решение инспектора\n(comment обязателен)
        PENDING --> NEGATIVE_VERIFIED2: решение инспектора\n(reason_code + comment обязательны)
        PENDING --> CLARIFICATION_REQUIRED2: решение инспектора
    }
```

Важное правило (`backend/CLAUDE.md` №6): **`CONFIRMED_VIOLATION` может поставить только человек** через
`POST /findings/{id}/decision`. Движок проверок (`engine`) никогда не присваивает этот статус — максимум
`CANDIDATE`. Составной `finding` инспектор может разбить на атомарные (`POST /findings/{id}/split`) —
новые записи ссылаются на исходную через `parent_finding_id`.

Каждое решение атомарно (backend-plan.md §8.8): обновление `finding` + запись `audit_log` + запись
`dataset_items` (черновик GOLD: `CONFIRMED_VIOLATION` → положительный пример, `NEGATIVE_VERIFIED` →
отрицательный; `CLARIFICATION_REQUIRED` в GOLD не попадает) — одна транзакция через `db.WithTx`.

## Инкрементальный пересчёт

При дозагрузке файлов после `READY`/`VERIFYING`/`COMPLETED` пересчитываются только затронутые
`(stage, discipline)` пары, а не весь процесс заново (требование ТЗ: ≤ 1 мин на пересчёт):

- Для `evidence_group` с неизменным `input_hash` (хеш фактов и файлов, входящих в группу) — решение
  инспектора сохраняется как есть.
- Для группы с изменившимся `input_hash` — создаётся новый `finding` с `inspector_status=PENDING`,
  старый архивируется через `superseded_by`. Предыдущая версия протокола не удаляется.

## Событие Kafka: от бизнес-транзакции до DLQ

```mermaid
sequenceDiagram
    participant Domain as Доменный код (api/engine)
    participant PG as PostgreSQL
    participant Relay as cmd/relay
    participant Kafka
    participant Consumer as Consumer (engine/rin-sync/воркер)

    Domain->>PG: BEGIN
    Domain->>PG: бизнес-изменение + INSERT outbox_events (status=pending)
    Domain->>PG: COMMIT
    Note over Domain,PG: WithTx — одна транзакция (architecture.md#unit-of-work)

    loop опрос
        Relay->>PG: SELECT ... FOR UPDATE SKIP LOCKED LIMIT 100
        Relay->>PG: UPDATE status='publishing'
        Relay->>Kafka: publish
        alt delivery report OK
            Relay->>PG: UPDATE status='published'
        else ошибка
            Relay->>PG: UPDATE status='failed', available_at += backoff
        end
    end

    Kafka->>Consumer: poll
    Consumer->>PG: BEGIN
    Consumer->>PG: INSERT consumed_events ON CONFLICT DO NOTHING
    alt уже обработано (0 строк)
        Consumer->>PG: COMMIT
        Consumer->>Kafka: commit offset (no-op бизнес-логики)
    else новое событие
        Consumer->>PG: бизнес-логика + INSERT outbox_events (если нужно)
        Consumer->>PG: COMMIT
        Consumer->>Kafka: commit offset
    end

    Note over Consumer,Kafka: после 3 неудачных попыток с backoff —\npublish в &lt;topic&gt;.dlq, offset коммитится
```

Полное описание топиков и конверта события — [architecture.md#событийная-архитектура](architecture.md#событийная-архитектура).

## HTTP-запрос

```mermaid
sequenceDiagram
    participant Client
    participant Caddy
    participant MW as middleware chain
    participant Handler
    participant PG as PostgreSQL

    Client->>Caddy: HTTPS запрос
    Caddy->>MW: reverse proxy (host.docker.internal:8080)
    MW->>MW: RequestID (генерирует/пробрасывает X-Request-ID)
    MW->>MW: Recovery (паника → 500 через httpx.ErrorResponse)
    MW->>MW: Metrics (http_requests_total, http_request_duration_seconds)
    MW->>Handler: вызов хендлера
    Handler->>PG: запрос/транзакция
    Handler-->>MW: результат или *httpx.AppError
    MW->>MW: Logger (лог завершения запроса: request_id, статус, длительность)
    MW-->>Client: JSON-ответ ({"error":{...},"request_id":...} при ошибке)
```

Реализация порядка middleware — `cmd/api/main.go`: `RequestID → Recovery → Logger → Metrics`.

## Жизненный цикл сервиса (`cmd/api`)

```mermaid
sequenceDiagram
    participant Main as main()
    participant Cfg as config.Load
    participant Log as logging.New
    participant DB as db.New
    participant HTTP as httpx server

    Main->>Log: логгер по умолчанию (info)
    Main->>Cfg: config.yaml + ENV
    Main->>Log: пересоздать логгер с cfg.Logger.Level
    Main->>Main: signal.NotifyContext(SIGINT, SIGTERM)
    Main->>DB: подключение + health-check
    Main->>Main: metrics.New() + отдельный HTTP-сервер :9091/metrics
    Main->>HTTP: gin engine + middleware + /healthz /readyz /swagger
    Main->>HTTP: server.Start() (неблокирующий)
    Main->>Main: select { <-ctx.Done() | <-server.Notify() }
    Note over Main: сигнал ОС или ошибка сервера
    Main->>HTTP: server.Shutdown(ShutdownTimeout)
    Main->>DB: database.Close()
```

Тот же принцип применяется (и будет применяться при реализации M1+) к `cmd/relay`/`cmd/engine`:
на сигнал завершения — `consumer.Close()` и `producer.Flush()` перед выходом процесса, чтобы не терять
недокоммиченные офсеты и недоставленные сообщения.

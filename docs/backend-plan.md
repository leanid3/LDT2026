# План backend-части «Инспектор ИИ»

Документ для CLI-агента (Claude Code) и участников 1 и 2. Описывает, что строим, какие требования ТЗ закрываем,
что переиспользуем из прошлого проекта и в каком порядке делать задачи.

Источники требований (положить в `docs/source/`):

- `10__Мосстройнадзор.pdf` — техническое задание (далее ТЗ, ссылки на пункты вида «ТЗ 9.2»);
- `Матрица_параметров_редакция1_1.xlsx` — листы МАТРИЦА (132 параметра), СХЕМА GOLD, МЕТРИКИ, ПРИМЕРЫ РАЗМЕТКИ;
- `Перечень_исполнительной_документации_редакция1_1.docx` — обязательный реестр файлов и правила выбора редакции;
- `Комплект_предметной_разметки_с_пояснениями_.pdf` — пример нарушений (ОВ, сравнение по помещениям).

Ответственные: **У1** — участник 1 (DevOps/Go), **У2** — участник 2 (Go). Агент работает на задачах обоих.

---

## 1. Что требует ТЗ от backend (сводка)

| Требование | Пункт ТЗ | Где реализуем |
|---|---|---|
| REST over HTTPS, JSON, OpenAPI 3.0 с валидацией схемы | 1.3 | `contracts/openapi.yaml`, oapi-codegen, middleware валидации |
| Асинхронная pull-модель: загрузка → `process_id` → опрос статуса | 1.4 | `cmd/api`, статусы процесса |
| Форматы PDF, DOCX, XML; 50 МБ на файл; 200 МБ на пакет; битые файлы отклонять | 9.1 | upload + confirm |
| При таймауте обработки — до 2 повторов, затем уведомление администратора | 9.1 | engine: контроль заданий парсинга + алерт |
| Реестр файлов обязателен; без него — `CLARIFICATION_REQUIRED` | docx | registry |
| Выбор актуальной утверждённой редакции; устаревшая не может быть эталоном | 9.1, docx | revision selector |
| Статусы загрузки PD/RD/ID × UPLOADED/PARTIAL/MISSING | 9.1 | completeness |
| Статусы процесса PENDING → PARSING → READY → VERIFYING → COMPLETED → FINALIZED | 9.1 | process state machine |
| Дозагрузка без перезагрузки до финализации, инкрементальный пересчёт ≤ 1 мин | 9.1, 9.2, 11 | incremental |
| Сценарии FULL, PD_RD_ONLY, PD_ID_ONLY, RD_ID_ONLY, SINGLE_ONLY, PARTIALLY_LOADED | 9.2 | engine |
| Для каждого из 132 параметров: применимость → комплектность → актуальность → сравнение | 9.2 | engine |
| Статусы findings: NEGATIVE_VERIFIED, CANDIDATE, CONFIRMED_VIOLATION, MISSING_EVIDENCE, NOT_APPLICABLE, NOT_COMPARABLE, CLARIFICATION_REQUIRED, SUSPICION | 9.2 | engine, verification |
| Карточка доказательства (finding_id, код, expected/actual, file_id + SHA-256, стадия, шифр, редакция, статус утверждения, лист/страница, bbox, обоснование, риск, решение, причина) | 9.2 | evidence, protocol |
| bbox нормализован в [0;1] с учётом CropBox/MediaBox/Rotate | 9.1 | контракт `ExtractedFact` (считают воркеры), backend валидирует диапазон |
| Протокол: 5 раздельных таблиц, версии Матрицы/данных/модели, экспорт JSON/PDF (DOCX/XML — дашборд) | 9.2, 7 | protocol |
| Верификация: подтвердить / отклонить (reason_code + комментарий) / уточнение; разбиение составного finding | 9.3 | verification API |
| Финализация только после обработки всех CANDIDATE; отмена — админ/супервизор, в аудит | 9.3 | process |
| GOLD: CONFIRMED_VIOLATION и NEGATIVE_VERIFIED с полной карточкой, split по object_id | 9.4, 14 | dataset |
| SUSPICION (свободный поиск) — отдельно, не нарушение | 9.5 | suspicions |
| Передача в ИАИС «РиН» только после PROTOCOL_FINALIZED, ретраи 1/5/15 мин, PENDING_SYNC | 9.6 | rin-sync |
| Логин/пароль, роли инспектор / администратор / ML-инженер, аудит всех действий | 12 | auth, audit |
| Антивирусная проверка загружаемых файлов | 12.11 | ClamAV в confirm |
| JSON-логи: timestamp, level, service, message, request_id, user_id | 13.1 | slog middleware |
| Prometheus + Grafana, ELK, алерты e-mail/Telegram | 13 | infra (У1) |
| API p95 ≤ 200 мс, сравнение 132 параметров ≤ 2 мин, протокол ≤ 30 с | 11 | всё тяжёлое — асинхронно |

Отступления от ТЗ (осознанные, описать в `docs/arch.md`): Go вместо Node.js, Kafka вместо RabbitMQ.

---

## 2. Что переиспользуем из `leanid3/task-manager-docs`

Код копируется в `backend/` (без `.git`), модуль переименовывается из `app` в
`github.com/Tanoklllmonbku/Hakaton-LDT/backend`.

### Берём

- Структура `cmd/`, `internal/`, `pkg/`, `config/`, `Makefile`, `Dockerfile`, `.env.example`, CI-конфиг
  (изначально `.gitea`, при переносе перешли на `.github/workflows/` — GitHub Actions, см. `docs/deployment.md`).
- gin, validator, pgx/v5, cleanenv, minio-go/v7, confluent-kafka-go/v2.
- Адаптеры Postgres, MinIO, Kafka producer/consumer.
- testcontainers-go с модулями postgres, kafka, minio.
- JSON-логгер, эндпоинт метрик, health.

### Обязательно исправить при переносе

| Проблема | Исправление |
|---|---|
| `consumer_auto_commit: true` | `enable.auto.commit=false`, ручной `CommitMessage` после COMMIT транзакции |
| `consumer_max_poll_records` (параметр Java-клиента, в librdkafka отсутствует) | Удалить. Добавить `max.poll.interval.ms` (≥ 1 800 000) и `fetch.max.bytes` |
| swaggo/swag (генерирует Swagger 2.0) | Удалить аннотации и swaggo. Spec-first OpenAPI 3.0 + oapi-codegen (gin-server). Swagger UI отдавать статикой по `contracts/openapi.yaml` |
| `config.yaml` с паролями в репозитории | В `.gitignore`. Оставить `config.example.yaml`. Секреты из ENV |
| Логи в файлы с ротацией | `mode: stdout`, JSON. Файловый режим удалить |
| Топик `tasks_llm` | Топики из раздела 6 |

### Проверить в перенесённом коде

1. Producer обрабатывает delivery reports (канал `Events()` / `Flush` с проверкой ошибок), а не только `Produce`.
2. Graceful shutdown: `consumer.Close()`, `producer.Flush()`, остановка HTTP с таймаутом.
3. MinIO: есть presigned **POST** policy с `SetContentLengthRange` (иначе лимит 50 МБ не проверяется хранилищем).
4. Транзакции pgx пробрасываются так, чтобы outbox-запись и бизнес-изменение шли в одной транзакции (unit of work: `WithTx(ctx, func(tx pgx.Tx) error)`).

### Дописать (в каркасе нет)

Outbox и relay, идемпотентность consumer'ов, DLQ, проверка файлов (ClamAV, magic bytes, pdfcpu),
реестр и выбор редакции, auth с ролями, аудит, весь доменный слой проверок и протокола.

---

## 3. Структура репозитория

```
Hakaton-LDT/
├── contracts/                    # общие контракты (владелец контрактов — участник 4, API — У2)
│   ├── openapi.yaml              # OpenAPI 3.0
│   ├── events/*.schema.json      # JSON Schema событий Kafka
│   ├── facts.schema.json         # ExtractedFact (выход LLM-воркера)
│   └── layout.schema.json        # DocumentLayout (выход parse-воркера)
├── backend/
│   ├── CLAUDE.md
│   ├── cmd/
│   │   ├── api/                  # HTTP API
│   │   ├── relay/                # outbox → Kafka
│   │   ├── engine/               # consumer результатов воркеров, движок проверок, контроль заданий
│   │   ├── rin-sync/             # отправка в ИАИС «РиН» с ретраями
│   │   ├── rin-mock/             # заглушка ИАИС «РиН» для демо
│   │   └── tools/                # import-matrix, seed-users, export-dataset
│   ├── internal/
│   │   ├── config/
│   │   ├── platform/             # общее: db (pgx, WithTx), kafka, outbox, idempotency, minio, logging, metrics, httpx
│   │   ├── auth/                 # логин, JWT, роли, middleware
│   │   ├── audit/                # Audit_Log + middleware
│   │   ├── objects/              # Objects
│   │   ├── files/                # загрузка, проверка, Files, реестр, выбор редакции, комплектность
│   │   ├── process/              # процесс проверки, машина состояний, сценарии
│   │   ├── matrix/               # Params, импорт xlsx, правила сравнения (rules/*.yaml)
│   │   ├── facts/                # приём ExtractedFact от воркеров
│   │   ├── engine/               # оркестрация проверок, rule evaluators, evidence groups, инкрементальность
│   │   ├── findings/             # findings, верификация, split, решения
│   │   ├── suspicions/
│   │   ├── protocol/             # версии, сборка, экспорт JSON/PDF/XML
│   │   ├── dataset/              # Dataset_Items, экспорт GOLD
│   │   ├── rin/                  # клиент и синхронизация
│   │   └── transport/http/       # сгенерированные интерфейсы + реализации хендлеров
│   ├── migrations/
│   ├── rules/                    # конфиги правил по кодам Матрицы (YAML)
│   ├── templates/                # HTML-шаблоны протокола для PDF
│   ├── config.example.yaml
│   ├── Dockerfile                # один образ, бинарь выбирается командой
│   └── Makefile
├── workers/                      # Python (участники 4, 5) — не трогать
├── frontend/                     # React (участник 3) — не трогать
├── infra/                        # docker-compose, prometheus, grafana, alertmanager, filebeat/elk (У1)
└── docs/
    ├── source/                   # документы заказчика
    ├── arch.md
    └── backend-plan.md           # этот файл
```

Все бинари собираются в один образ. В docker-compose — разные сервисы с разной командой.

---

## 4. Доменная модель и статусы

### 4.1 Enum-значения (строго как в ТЗ)

```
doc_stage:            PD | RD | ID
approval_status:      DRAFT | APPROVED | FOR_CONSTRUCTION | SUPERSEDED | CANCELLED
upload_status:        PD_UPLOADED | PD_PARTIAL | PD_MISSING | RD_UPLOADED | RD_PARTIAL | RD_MISSING
                      | ID_UPLOADED | ID_PARTIAL | ID_MISSING
process_status:       PENDING | PARSING | READY | VERIFYING | COMPLETED | FINALIZED
scenario:             FULL | PD_RD_ONLY | PD_ID_ONLY | RD_ID_ONLY | SINGLE_ONLY | PARTIALLY_LOADED
finding_status:       NEGATIVE_VERIFIED | CANDIDATE | CONFIRMED_VIOLATION | MISSING_EVIDENCE
                      | NOT_APPLICABLE | NOT_COMPARABLE | CLARIFICATION_REQUIRED | SUSPICION
inspector_status:     PENDING | CONFIRMED_VIOLATION | NEGATIVE_VERIFIED | CLARIFICATION_REQUIRED
protocol_status:      DRAFT | VERIFICATION_COMPLETED | PROTOCOL_FINALIZED
sync_status:          NOT_REQUIRED | PENDING_SYNC | SYNCED | SYNC_FAILED
review_priority:      HIGH | MEDIUM | LOW          # только очередность проверки, не юр. действие
reject_reason_code:   WRONG_REVISION | APPROVED_CHANGE | OCR_ERROR | LINKING_ERROR
                      | NOT_APPLICABLE | DUPLICATE | OTHER
file_check_status:    UPLOADING | UPLOADED | REJECTED_FORMAT | REJECTED_SIZE | REJECTED_CORRUPTED
                      | REJECTED_VIRUS | ACCEPTED
parse_quality:        OK | LOW_QUALITY | ABSTAIN | FAILED
roles:                inspector | supervisor | admin | ml_engineer
```

### 4.2 Машина состояний процесса (ТЗ 9.1, 9.3)

```
PENDING   --start check-------------------> PARSING
PARSING   --все задания парсинга/извлечения
            завершены, протокол собран-----> READY
READY     --первое решение инспектора-----> VERIFYING
VERIFYING --нет inspector_status=PENDING
            у CANDIDATE --------------------> COMPLETED
COMPLETED --finalize (inspector/supervisor)-> FINALIZED
FINALIZED --unfinalize (supervisor/admin,
            обязательная причина, аудит)----> COMPLETED
READY|VERIFYING|COMPLETED --дозагрузка файлов--> PARSING (инкрементально) --> VERIFYING/READY
```

Дозагрузка: разрешена в PENDING, READY, VERIFYING, COMPLETED. В PARSING — 409 с `Retry-After`. В FINALIZED — 409.
Верификация: разрешена в READY, VERIFYING, COMPLETED (до финализации).
Финализация: только если у всех findings со статусом CANDIDATE `inspector_status != PENDING`
(допускается перевод в CLARIFICATION_REQUIRED). MISSING_EVIDENCE выводится отдельным перечнем.

### 4.3 Evidence group и finding

- `evidence_group` = объект + атомарный параметр/правило (+ `element_key`, например помещение «140» или система «В2.4»)
  + набор актуальных редакций ПД/РД/ИД + доказательные фрагменты. Это единица протокола и метрик.
- `finding` — результат проверки evidence group со статусом и карточкой. Составной finding инспектор разбивает на атомарные.

---

## 5. Схема БД (миграции)

Черновик. Агент уточняет типы и индексы, но не меняет смысл полей ТЗ (раздел 10 ТЗ).
Поля листа «СХЕМА GOLD» из xlsx сверить при создании `dataset_items`.

> Реализовано заранее (до M1) как `backend/migrations/0001_platform` … `0009_facts_sha256_nullable`,
> черновиком по этой схеме — чтобы дать возможность проверять API и стенд локально, не дожидаясь
> согласованной финальной схемы. Домен (auth/objects/files/process/engine/findings/protocol/rin)
> реализован поверх этой схемы в рамках M1–M4, см. §11 ниже и `docs/architecture.md`. См.
> `docs/development.md#миграции`. Правки по мере согласования — только новыми миграциями (правило №2,
> `backend/CLAUDE.md`), не редактированием этих файлов.

```sql
-- 0001_platform
users(id uuid pk, login text unique, password_hash text, full_name text, role text, is_active bool, created_at)
outbox_events(event_id uuid pk, aggregate_id uuid, event_type text, topic text, msg_key text,
              payload jsonb, status text default 'pending', attempts int, available_at timestamptz,
              locked_at timestamptz, published_at timestamptz, last_error text, created_at timestamptz)
  -- индекс (status, available_at) where status in ('pending','failed','publishing')
consumed_events(consumer_name text, event_id uuid, processed_at timestamptz, pk(consumer_name, event_id))
idempotency_keys(key text pk, user_id uuid, request_hash text, response_status int, response_body jsonb,
                 created_at timestamptz, expires_at timestamptz)
audit_log(id bigserial pk, user_id uuid, action text, object_type text, object_id text,
          details jsonb, before jsonb, after jsonb, ip inet, user_agent text, request_id text, ts timestamptz)
monitoring_metrics(...)  -- по ТЗ; фактически метрики в Prometheus, таблицу создать минимальной

-- 0002_objects_files
objects(id uuid pk, external_id text unique, name, address, customer, contractor, permit_number, created_at)
processes(id uuid pk, object_id uuid fk, status text, scenario text, matrix_version text,
          created_by uuid, created_at, updated_at, finalized_at, finalized_by)
files(id uuid pk, object_id uuid, process_id uuid, original_name text, storage_key text,
      size_bytes bigint, sha256 char(64), content_type text, page_count int,
      check_status text, check_error text,
      doc_stage text, discipline text, document_code text, revision text,
      approval_status text, approval_date date, sheet_page_range text,
      predecessor_id uuid null, successor_id uuid null, signature_status text,
      is_current bool, selection_status text, selection_reason text,
      uploaded_at timestamptz)
  -- UNIQUE(process_id, sha256): повторная загрузка того же содержимого не создаёт дубль
  -- перезапись под тем же id запрещена: files только INSERT, изменяются лишь служебные статусы
registry_entries(id, process_id, raw jsonb, file_id uuid null, match_status text)   -- строки реестра до сопоставления
upload_status(process_id, stage, status, pk(process_id, stage))

-- 0003_parsing
parse_jobs(id uuid pk, process_id, file_id, page_from int, page_to int, status text,
           attempts int, quality text, layout_key text, started_at, finished_at, last_error)
extract_jobs(id uuid pk, process_id, params text[], status, attempts, facts_key text, started_at, finished_at, last_error)

-- 0004_matrix
params(id int pk, code varchar(20) unique,      -- 'M-001'..'M-132' (канонический код из xlsx)
       alias_code varchar(20),                   -- код из примеров ТЗ ('KR-55' и т.п.), может быть null
       section, parameter_name, unit, source_pd, source_rd, source_id, trigger_logic,
       review_priority, sp_reference, gost_reference, fz_reference, other_normative,
       data_type, min_value float, max_value float, regex_pattern, is_active bool,
       matrix_version text, created_at, updated_at)
normative_base(id, document_name, document_number, section, parameter_name, min_value, max_value,
               effective_from, effective_to)
logical_rules(id, rule_name, condition jsonb, expected jsonb, normative_base text, is_active)

-- 0005_checks
facts(id uuid pk, process_id, object_id, file_id, file_sha256, stage, param_code, rule_code,
      element_key text, value_raw text, value_norm jsonb, unit, page int, bbox float8[4],
      quote text, confidence float, method text, extractor_version text, has_change_notice bool)
evidence_groups(id uuid pk, process_id, object_id, param_code, rule_code, element_key, input_hash text)
checks(id uuid pk, process_id, param_id, object_id, evidence_group_id,
       expected_value text, actual_value text, delta text,
       completeness_status text, finding_status text, review_priority text,
       rationale text, risk_level text, created_at, superseded_by uuid null)
evidence_fragments(id uuid pk, evidence_group_id, file_id, stage, sheet_page int,
                   bbox_polygon_norm jsonb, extracted_value text, quote text,
                   role_expected_actual text)   -- 'expected' | 'actual'
findings(id uuid pk, check_id uuid, parent_finding_id uuid null,  -- для split
         finding_status text, inspector_status text default 'PENDING',
         decided_by uuid, decided_at, reason_code text, comment text, version int)
suspicions(id uuid pk, process_id, object_id, discovery_method, confidence, description,
           pd_reference, rd_reference, review_priority, normative_base, finding_status 'SUSPICION',
           inspector_status, evidence jsonb)

-- 0006_protocol
protocols(id uuid pk, process_id, object_id, version int, status text,
          matrix_version, dataset_version, model_version, input_manifest_hash char(64),
          payload_key text, created_at, finalized_at, sync_status text)
rejection_log(id, violation_id, rejection_reason, ai_verdict, suggested_fix, retraining_status)
dispute_log(id, violation_id, inspector_comment, ai_comment, resolution_status, resolved_by)

-- 0007_ml
dataset_items(id, evidence_group_id, gold_label, expert_id, reason_code, dataset_version, split,
              object_group_id, created_at)
model_versions(model_version pk, artifact_hash, dataset_version, metrics_json, approval_status,
               approved_by, deployed_at, rollback_to)
ml_retraining_log(id, model_version, dataset_version, split_hashes, precision, recall, f1,
                  false_positive_rate, per_category_metrics, approval_status, approved_by)
```

---

## 6. Kafka

### 6.1 Общий конверт события

```json
{
  "event_id": "uuid",
  "event_type": "doc.parse.requested",
  "schema_version": 1,
  "occurred_at": "RFC3339",
  "correlation_id": "process_id",
  "object_id": "uuid",
  "process_id": "uuid",
  "data": { }
}
```

Заголовки Kafka: `correlation_id`, `event_type`, `trace_id` (если есть). Ключ сообщения — `object_id`.
В `data` только ссылки на MinIO, не содержимое (лимит сообщения ~1 МБ).

### 6.2 Топики (3 партиции, retention 7 дней; DLQ — 1 партиция, 30 дней)

| Топик | Producer | Consumer | data |
|---|---|---|---|
| `doc.parse.requested` | relay (из api) | parse-воркер (Python) | file_id, sha256, storage_key, content_type, page_from, page_to, stage, discipline, job_id |
| `doc.parse.completed` | parse-воркер | engine | job_id, file_id, page_from, page_to, quality, layout_key, stats |
| `doc.extract.requested` | relay (из engine) | LLM-воркер (Python) | job_id, process_id, files[{file_id, stage, discipline, layout_key}], params[] (подмножество кодов при инкрементальном пересчёте) |
| `doc.extract.completed` | LLM-воркер | engine | job_id, facts_key (MinIO, JSONL по `contracts/facts.schema.json`), suspicions_key, model_version |
| `protocol.ready` | relay (из engine) | (уведомления, опционально WebSocket) | process_id, protocol_version |
| `rin.sync.requested` | relay (из api при финализации) | rin-sync | process_id, protocol_id |
| `*.dlq` | любой consumer после N попыток | алерт, ручной replay | исходное событие + ошибка |

### 6.3 Правила consumer'а (пакет `internal/platform/kafka`)

- `enable.auto.commit=false`, `max.poll.interval.ms` ≥ 30 минут, `isolation.level=read_committed`.
- Обработка: `BEGIN` → `INSERT consumed_events ON CONFLICT DO NOTHING` (0 строк → уже обработано, commit offset) →
  бизнес-логика → `INSERT outbox_events` (если нужно) → `COMMIT` → commit offset.
- Ошибка: до 3 попыток с backoff внутри consumer, затем публикация в `<topic>.dlq` и commit offset.

### 6.4 Relay (`cmd/relay`)

- Несколько реплик. Claim пачки: `FOR UPDATE SKIP LOCKED LIMIT 100`, статус `publishing`, `locked_at`.
- Публикация с ожиданием delivery report → `published`. Ошибка → `failed`, `available_at` с экспоненциальным backoff (до 60 с).
- Зависшие `publishing` старше 2 минут забираются повторно. `event_id` не меняется при ретраях.

---

## 7. HTTP API (OpenAPI 3.0, префикс `/api/v1`)

Все изменяющие запросы принимают заголовок `Idempotency-Key`. Ошибки — единый формат
`{ "error": { "code": "...", "message": "...", "details": {...} }, "request_id": "..." }`.

### Auth
- `POST /auth/login` → access token (JWT, 12 ч для демо) · `GET /auth/me`

### Объекты и процессы
- `POST /objects` · `GET /objects` (фильтры: статус, раздел, даты; цвет green/yellow/red) · `GET /objects/{id}`
- `POST /documents/upload` — создать процесс (или дозагрузка в существующий по `process_id`), заявленный список файлов
  с размерами → `process_id` + presigned POST для каждого файла. Проверка лимитов 50/200 МБ по заявленным размерам.
- `POST /documents/{process_id}/confirm` — подтвердить загруженные файлы (проверки раздела 8.1)
- `POST /documents/{process_id}/registry` — загрузить реестр (CSV/XLSX/JSON)
- `POST /processes/{id}/start` — запустить проверку (PENDING → PARSING)
- `GET /processes/{id}` — статус процесса, сценарий, статусы загрузки, прогресс заданий, версия протокола
- `GET /processes/{id}/files` — файлы с редакциями, selection_status, check_status

### Протокол и верификация
- `GET /processes/{id}/protocol?version=` — JSON протокола (5 таблиц)
- `GET /processes/{id}/protocol/export?format=pdf|json|xml|docx`
- `GET /findings/{id}` — карточка доказательства (с presigned GET на страницы файлов)
- `POST /findings/{id}/decision` — `{ decision: CONFIRMED_VIOLATION|NEGATIVE_VERIFIED|CLARIFICATION_REQUIRED, reason_code?, comment }`
  (reason_code и comment обязательны для NEGATIVE_VERIFIED; comment обязателен для CONFIRMED_VIOLATION)
- `POST /findings/{id}/split` — разбить на атомарные findings (список частей с собственными доказательствами)
- `GET /processes/{id}/suspicions` · `POST /suspicions/{id}/promote` (в CANDIDATE — только с доказательствами)
- `POST /processes/{id}/finalize` · `POST /processes/{id}/unfinalize` (`{reason}`, роли supervisor/admin)

### Интеграция (ТЗ 9.6)
- `POST /inspection/{process_id}` — передача результатов во внешнюю ИС (только PROTOCOL_FINALIZED; ставит PENDING_SYNC)
- `GET /processes/{id}/sync` — статус синхронизации

### Администрирование
- `GET/POST/PATCH /admin/params` — Матрица (is_active, пороги, ссылки на нормативы) без перекодирования
- `GET/POST/PATCH /admin/normative`, `/admin/rules`
- `GET /admin/audit` — журнал аудита
- `GET /ml/dataset/export?dataset_version=` (роль ml_engineer) · `GET /ml/report/weekly`

### Служебное
- `GET /healthz`, `GET /readyz`, `GET /metrics` (отдельный порт), Swagger UI по `contracts/openapi.yaml`

---

## 8. Бизнес-логика

### 8.1 Приём файла (`files`, У1)

1. `upload`: проверить расширение из списка (pdf, docx, xml), заявленный размер ≤ 50 МБ, сумма ≤ 200 МБ.
   Создать `files` со статусом UPLOADING, выдать presigned POST с `content-length-range` и сроком 30 мин.
2. `confirm`: для каждого файла одним потоком из MinIO: `io.TeeReader` → sha256 + clamd INSTREAM.
   Затем magic bytes (`%PDF-`, ZIP с `word/document.xml`, `<?xml`). Для PDF — валидация pdfcpu и число страниц.
   Итог: ACCEPTED или REJECTED_* с сообщением из таблицы ошибок ТЗ 9.1. Вирус — удалить объект из MinIO.
3. Дубликат по sha256 в том же процессе — вернуть существующий file_id, не создавать новую запись.

### 8.2 Реестр и выбор редакции (`files/registry`, У1)

Поля реестра: object_id, file_id/file_name, sha256, doc_stage, discipline, document_code, revision, approval_status,
approval_date, sheet_page_range, predecessor_id/successor_id, signature_status.

- Сопоставление строки реестра с файлом: по sha256, иначе по имени файла. Расхождение хеша — ошибка строки.
- Нет реестра у процесса → все файлы `selection_status=CLARIFICATION_REQUIRED`, причина `REGISTRY_MISSING`.

Алгоритм выбора (на группу `object_id + doc_stage + discipline + document_code`):
1. Построить граф по predecessor/successor. Цикл или ссылка на отсутствующую запись → вся группа CLARIFICATION_REQUIRED.
2. SUPERSEDED и CANCELLED — исключить из сравнения, сохранить для аудита. DRAFT — не может быть эталоном.
3. Кандидаты: APPROVED или FOR_CONSTRUCTION без утверждённого потомка.
4. Ровно один → `is_current=true`. Ноль или больше одного → CLARIFICATION_REQUIRED с причиной.
5. Покрыть табличными тестами все строки таблицы «Правила выбора источника» из docx.

Статусы загрузки по стадии: нет файлов стадии → `*_MISSING`; есть, но часть групп CLARIFICATION_REQUIRED
или ожидаемые по реестру файлы не загружены → `*_PARTIAL`; иначе `*_UPLOADED`.

### 8.3 Запуск и контроль заданий (`engine`, У2)

- `start`: для каждого ACCEPTED файла с `is_current=true` нарезать PDF на `parse_jobs` по 20 страниц
  (DOCX/XML — одно задание), записать outbox `doc.parse.requested`. Статус процесса PARSING.
- Результаты кэшируются воркерами по sha256 — повторный файл обрабатывается быстро.
- Fan-in: когда все `parse_jobs` процесса в конечном статусе → создать `extract_jobs` и outbox `doc.extract.requested`.
- После `doc.extract.completed`: загрузить JSONL фактов из MinIO → `facts` (валидация по `facts.schema.json`,
  bbox в [0;1], page в диапазоне) → запуск движка проверок.
- Контроль таймаутов (тикер в `cmd/engine`, раз в 30 с): задание дольше порога → повтор (до 2 раз по ТЗ 9.1),
  затем FAILED, метрика `jobs_failed_total` и алерт администратору. Файлы с FAILED/ABSTAIN дают NOT_COMPARABLE,
  процесс не блокируется.

### 8.4 Сценарий (`process`, У2)

По наличию стадий среди текущих файлов: PD+RD+ID → FULL; PD+RD → PD_RD_ONLY; PD+ID → PD_ID_ONLY;
RD+ID → RD_ID_ONLY; одна стадия → SINGLE_ONLY. Если хотя бы одна стадия `*_PARTIAL` → PARTIALLY_LOADED.

### 8.5 Движок проверок (`engine`, У2)

Для каждого активного параметра Матрицы (и каждого `element_key`, если параметр поэлементный):

1. **Применимость.** Правило из `rules/<code>.yaml` отсутствует → NOT_COMPARABLE с причиной
   `RULE_NOT_IMPLEMENTED` (честно, не нарушение). Параметр неприменим к объекту (например, газ отсутствует) →
   NOT_APPLICABLE с основанием.
2. **Комплектность.** Какие стадии нужны правилу (из колонок «Источник в ПД/РД/ИД» и `rules/*.yaml`).
   Стадия не загружена и по правилу не обязательна → NOT_APPLICABLE для этой пары.
   Обязательный источник отсутствует → MISSING_EVIDENCE.
3. **Актуальность.** Источник из группы с CLARIFICATION_REQUIRED → CLARIFICATION_REQUIRED.
4. **Сопоставимость.** Нет фактов или только LOW_QUALITY/ABSTAIN → NOT_COMPARABLE.
5. **Сравнение** по типу правила (ниже). Расхождение → CANDIDATE с evidence fragments обеих сторон.
   Расхождения нет → NEGATIVE_VERIFIED (предварительный отрицательный результат).
6. Если у факта `has_change_notice=true` (рядом отметка «Изм.» или облако изменений) — CANDIDATE всё равно создаётся,
   но в карточку добавляется флаг `change_notice` и подсказка «проверить согласованное изменение».
   Статус CONFIRMED_VIOLATION движок не ставит никогда.

Типы правил (`rules/*.yaml`, поле `type`):

| type | Смысл | Пример из Матрицы |
|---|---|---|
| `numeric_equal` | расхождение > 0 | M-001 площадь застройки |
| `numeric_delta_pct` | \|a−b\|/a > порог | M-002 общая площадь > 1% |
| `numeric_decrease` | значение в РД/ИД меньше ПД | M-012 машино-места |
| `threshold_min` / `threshold_max` | нарушение абсолютного порога | M-041 ширина двери < 0,9 м; M-030 проезд < 4,2 м |
| `ordinal_decrease` | понижение по упорядоченной шкале | M-055 класс бетона, M-057 арматура, M-056 сталь, M-022 огнестойкость, M-021 класс энергоэффективности |
| `set_difference` | элементы ПД отсутствуют в РД/ИД (по element_key) | нарушения ОВ из разметки: системы по помещениям |
| `presence` | элемент есть в ПД, отсутствует в РД | тёплый пол в помещениях МГН |
| `tolerance` | факт отклонения > допуска на исполнительной схеме | пример OKT103 |
| `text_mismatch` | нормализованные строки различаются | M-052 цвет/артикул облицовки |

Шкалы для `ordinal_decrease` хранить в `rules/scales.yaml`:
бетон B7.5 … B60; арматура A240 < A400 < A500 < A600; сталь C235 < C245 < C255 < C345 < C390 < C440;
огнестойкость V < IV < III < II < I; КПО С3 < С2 < С1 < С0; энергоэффективность G < F < E < D < C < B < A < A+ < A++.

Начальный набор правил (15–25 кодов) согласовать с участником 5 — по тем параметрам, для которых в выданных
документах реально есть данные. Остальные дают NOT_COMPARABLE с причиной.

Результат записывается атомарно: новая версия `checks`/`findings`, старые помечаются `superseded_by`.
Для каждой evidence group считается `input_hash` (хеши фактов и файлов) — нужен для инкрементальности.

### 8.6 Инкрементальный пересчёт (У1 помогает У2)

- Индекс «параметр → (stage, discipline)» строится при импорте Матрицы из колонок источников и `rules/*.yaml`.
- При дозагрузке: пересчитать выбор редакций; определить затронутые (stage, discipline); отправить на parse только новые
  файлы; в `doc.extract.requested` передать только затронутые параметры.
- Для evidence group с неизменным `input_hash` решение инспектора сохраняется. С изменённым — новый finding с
  `inspector_status=PENDING`, старый архивируется со ссылкой. Предыдущая версия протокола сохраняется.

### 8.7 Протокол (`protocol`, У2; экспорт — У1)

JSON протокола:
- шапка: объект, процесс, версия протокола, `matrix_version`, `dataset_version`, `model_version`,
  `input_manifest_hash` (sha256 от отсортированного списка sha256 входных файлов), дата;
- раздел «Статус загрузки документов» и «Тип проверки» (сценарий);
- таблицы: (1) комплектность и сопоставимость (MISSING_EVIDENCE, NOT_APPLICABLE, NOT_COMPARABLE, CLARIFICATION_REQUIRED);
  (2) предварительные кандидаты; (3) подтверждённые нарушения; (4) проверенные отрицательные результаты; (5) гипотезы SUSPICION;
- карточка доказательства для каждого кандидата — все поля из ТЗ 9.2.

Экспорт: PDF через gotenberg (HTML-шаблон из `templates/` → PDF, миниатюры страниц с отрисованным bbox),
XML — прямая сериализация, DOCX — опционально (pandoc html→docx), JSON — как есть.
Генерация ≤ 30 с (ТЗ 11). Протокол сохраняется в MinIO (`payload_key`) и в таблицу `protocols`.

### 8.8 Верификация и финализация (`findings`, У2)

- Решение атомарно: обновить finding, записать `audit_log`, записать `dataset_items` (черновик GOLD:
  CONFIRMED_VIOLATION → положительный, NEGATIVE_VERIFIED → отрицательный; CLARIFICATION_REQUIRED в GOLD не идёт),
  при отклонении — `rejection_log`.
- Первое решение переводит процесс READY → VERIFYING. Все CANDIDATE решены → COMPLETED.
- `finalize`: проверка условия раздела 4.2 → протокол PROTOCOL_FINALIZED, процесс FINALIZED, outbox `rin.sync.requested`
  (если интеграция включена), `sync_status=PENDING_SYNC`.
- `unfinalize`: только supervisor/admin, обязательная причина, аудит → VERIFICATION_COMPLETED / COMPLETED.
- После финализации автоматическая дозагрузка не запускает проверку, а только помечает «есть новые документы»
  (ТЗ 9.6) и предлагает создать новый процесс.

### 8.9 Интеграция с ИАИС «РиН» (`rin`, У1)

- `cmd/rin-sync` потребляет `rin.sync.requested`, отправляет только CONFIRMED_VIOLATION + версии протокола, Матрицы,
  модели + реестр входных файлов.
- Ретраи на 5xx/таймаут: 1, 5, 15 минут (для демо — множитель в конфиге). Затем SYNC_FAILED + алерт.
  Сбой передачи не меняет FINALIZED и решения инспектора.
- УКЭП-подпись запросов — интерфейс `Signer` с заглушкой и `// TODO(TZ)`.
- `cmd/rin-mock` — простой HTTP-сервер, логирует запросы, по флагу отвечает 5xx для демонстрации ретраев.

### 8.10 Auth и аудит (У1)

- bcrypt, JWT (HS256, секрет из ENV). Роли: inspector, supervisor, admin, ml_engineer.
- Матрица доступа: инспектор — загрузка, просмотр, решения, финализация; supervisor — плюс отмена финализации;
  admin — всё плюс Матрица, нормативы, пользователи; ml_engineer — чтение, экспорт датасета, отчёты.
- Audit middleware: все изменяющие запросы + все решения + финализация/отмена + сбои. Поля ТЗ 12.4.

---

## 9. Контракты с Python-воркерами

### 9.1 ExtractedFact (`contracts/facts.schema.json`, JSONL в MinIO)

```json
{
  "fact_id": "uuid",
  "process_id": "uuid",
  "file_id": "uuid",
  "file_sha256": "hex64",
  "stage": "PD",
  "param_code": "M-055",
  "rule_code": null,
  "element_key": "140",
  "value_raw": "B30",
  "value_norm": {"kind": "ordinal", "scale": "concrete", "value": "B30"},
  "unit": "Класс",
  "page": 26,
  "bbox": [0.12, 0.40, 0.31, 0.44],
  "quote": "Бетон класса В30 W6 F150",
  "confidence": 0.91,
  "method": "vector_text | ocr | llm | vlm",
  "quality": "OK",
  "has_change_notice": false,
  "extractor_version": "llm-extract@0.3.1"
}
```

`value_norm.kind`: `number` (+ `unit_si`), `ordinal` (+ `scale`), `string`, `set` (+ `items[]`), `bool`, `tolerance` (+ `allowed`, `actual`).
Backend отвергает факт (с логом и метрикой), если: bbox вне [0;1] или x0 ≥ x1; page вне диапазона файла; нет quote.

### 9.2 Suspicion (JSONL)

Структура из ТЗ 9.5: `discovery_method`, `confidence`, `description`, `pd_reference`, `rd_reference`,
`review_priority`, `normative_base` + массив доказательств в формате evidence fragment.

---

## 10. Нефункциональные требования

- Логи: slog JSON, поля `timestamp, level, service, message, request_id, user_id, process_id`. DEBUG только в dev.
- Метрики (префикс `inspector_`): `http_requests_total`, `http_request_duration_seconds`, `outbox_pending`,
  `kafka_consume_errors_total`, `dlq_messages_total`, `jobs_duration_seconds{kind}`, `jobs_failed_total{kind}`,
  `engine_check_duration_seconds`, `findings_total{status}`, `rin_sync_total{result}`.
- Алерты (Alertmanager → Telegram + e-mail): CPU > 80%, p95 > 500 мс, DLQ > 0 за 5 мин, outbox_pending > 100 за 5 мин,
  jobs_failed_total растёт, rin SYNC_FAILED.
- Производительность: все тяжёлые операции асинхронны; GET-эндпоинты ≤ 200 мс p95 (индексы, пагинация).
- Безопасность: TLS 1.3 на reverse proxy (Caddy) перед API; ClamAV; шифрование MinIO SSE (по возможности);
  ежедневный бэкап Postgres (`pg_dump` в cron-контейнере) с хранением 30 дней.

---

## 11. Порядок задач

Отметка `[ ]` → `[x]` по мере выполнения. Критерий готовности (DoD) указан у каждой вехи.

### M0 — 21.09 · Каркас и стенд (У1)
- [x] Перенести task-manager-docs в `backend/`, переименовать модуль, исправления из раздела 2
- [x] `infra/docker-compose.yml`: postgres, minio (+ init бакетов), kafka KRaft (+ init топиков), kafka-ui, redis, clamav,
      gotenberg, prometheus, grafana, alertmanager, caddy
- [x] `internal/platform`: config, logging, db (`WithTx`), metrics, health, graceful shutdown
- [x] Makefile: up, down, gen, migrate, seed, lint, test, e2e; CI: lint + test + build (GitHub Actions,
      `.github/workflows/ci.yml` — изначально был `.gitea`, заменён на GitHub Actions вместе с добавлением
      остального пакета документации, см. `docs/`)
- [x] Черновик `contracts/openapi.yaml` (с У2 и У3), `make gen` работает
- **DoD:** `make up && make migrate && make run-api` — `/healthz` 200, kafka-ui показывает топики
  — проверено вручную: postgres/minio/kafka+11 топиков/kafka-ui/redis/clamav/gotenberg/prometheus/grafana/
  alertmanager/caddy поднимаются и healthy, `make migrate` не падает на пустых миграциях (появятся в M1),
  `/healthz`→200, `/readyz`→200 (реальный ping в postgres), `/metrics` отдаёт `inspector_*`, kafka-ui видит все топики.
  `go build/vet/test/lint ./...` — чисто.

> **Обновление 28.09 (интеграция с командой, code freeze).** Репозитории команды
> (`Kaiman30/Hakaton-LDT-backend`, `RenFall/Hakaton-LDT`) проанализированы: готовых воркеров и моделей в них
> нет (RenFall — только документы, Kaiman — AuthService/API-Gateway, дублирующие наш auth, и Python-каркас
> `ML-Service-Core` без коннекторов). Поэтому: **реальные Python-воркеры написаны в `workers/`**
> (parse + extract), зафиксирован контракт `contracts/events/*.schema.json` + `contracts/facts.schema.json`,
> реализованы `import-matrix` (132 параметра) и каталог из 71 правила, DLQ в Go-consumer'ах, обогащённый API
> для фронтенда ([frontend-api.md](frontend-api.md)). Отметки ниже обновлены.
>
> **Примечание к M1–M4 (внеплановый заход, дата не привязана к графику У1/У2):** по просьбе
> пользователя реализован весь конвейер одним заходом — от загрузки файлов до отправки в ИАИС «РиН» —
> вместо Python-воркеров и ИАИС «РиН» подставлены наши заглушки (`cmd/mockworkers`, `cmd/rin-mock`),
> потому что реальных внешних сторон ещё нет. Отмечено ниже честно: что реализовано, что осталось.
> Полный список того, что сознательно не сделано, и почему — [architecture.md#границы-текущей-реализации](architecture.md#границы-текущей-реализации).

### M1 — 22.09 · Платформа и приём файлов (У1)
- [x] Миграции 0001–0002 (фактически 0001–0009, включая точечные правки 0008/0009 — см. architecture.md)
- [x] `platform/outbox` + `cmd/relay`; `platform/idempotency`; `platform/kafka` consumer
      — до 3 попыток с backoff, затем `<topic>.dlq` (тело + `dlq_*` заголовки), commit только после
      успешной записи в DLQ; юнит-тесты и проверка живым прогоном. (Раньше ошибка обработчика молча
      пропускала сообщение — исправлено.)
- [x] upload / confirm: лимиты, presigned POST (`SetContentLengthRange`), sha256 + ClamAV одним
      потоком (`io.TeeReader`/`MultiWriter`), magic bytes (PDF/DOCX/XML), pdfcpu (validate + число
      страниц, с recover() от паник pdfcpu на битых файлах)
- [x] Интеграционные тесты testcontainers на outbox и confirm (реальный Postgres + реальный MinIO,
      включая полный HTTP presigned-upload)
- **DoD:** загрузка PDF через API → файл ACCEPTED → сообщение `doc.parse.requested` видно в kafka-ui
      — проверено вручную на живом стенде (см. ниже, единая проверка на M1–M4).

### M2 — 23.09 · Реестр, редакции, доступ (У1) · Матрица и процесс (У2)
- [x] У1: парсер реестра (CSV/XLSX/JSON), выбор редакции (`files.SelectCurrent`, табличные тесты —
      цикл, висячая ссылка, 0/1/2+ кандидата, SUPERSEDED/CANCELLED/DRAFT), статусы загрузки по стадии
- [x] У1: auth (bcrypt+JWT), роли, аудит (`audit.Log` внутри транзакций мутирующих операций)
- [x] У2: `tools/import-matrix` (лист МАТРИЦА, 132 строки, `matrix_version`) → `params` + `contracts/matrix.json`;
      идемпотентно, параметры, исчезнувшие из редакции, деактивируются (тесты на реальном xlsx)
- [x] У2: машина состояний процесса, сценарии (`ComputeScenario`), `start` (нарезка `parse_jobs` по
      20 страниц PDF), fan-in → `extract_jobs`
- **DoD:** комплект с реестром → правильные `is_current` и статусы — проверено вручную и тестами;
      `make seed` загружает 132 параметра Матрицы и пользователей.

### M3 — 24.09 · Сквозной прогон (У1 + У2 + воркеры)
- [x] У2: приём фактов (`engine.ProcessFacts`, валидация: quote, bbox в [0;1], x0<x1), движок проверок —
      8 типов: `numeric_equal`, `numeric_delta_pct`, `numeric_decrease`, `numeric_increase`,
      `threshold_min/max`, `ordinal_decrease` (шкалы `rules/scales.yaml`), `text_mismatch`;
      `set_difference`, `presence`, `tolerance` — **не реализованы**, дают `NOT_COMPARABLE`
- [x] У2: каталог `rules/M-XXX.yaml` — **71 из 132** параметров, выведен из колонки «Логика ИИ-связи»
      Матрицы, **черновик до подтверждения экспертом (TODO(TZ))**; для остальных 61 — честный
      `NOT_COMPARABLE/RULE_NOT_IMPLEMENTED` (§8.5 п.1)
- [x] Воркеры (Python, `workers/`): parse (PDF/DOCX/XML → layout с bbox; OCR-хук) и extract (44 regex-шаблона
      + опциональный LLM с проверкой цитат); 93 теста; контракт на JSON Schema, общие golden-примеры
      проверяются и на Go, и на Python
- [ ] У1: метрики движка/дашборды Grafana/алерты/ELK-Loki — не в этом объёме (задел уже есть:
      `inspector_http_*` метрики с M0, остальные из §10 — TODO)
- **DoD (адаптирован):** `scripts/e2e.py`: два реальных PDF (ПД и РД с заложенными расхождениями) →
      настоящие Python-воркеры → 5 `CANDIDATE` (площадь застройки, огнестойкость I→II, бетон B35→B30,
      толщина плиты, ширина двери 800 мм < 0,9 м) и 6 `NEGATIVE_VERIFIED`, у каждого — цитата, страница и
      bbox; полный маршрут через настоящую Kafka до `SYNCED`. Сценарий ОВ/помещения 140/142 из исходного
      DoD относится к реальным данным примера ТЗ — не воспроизводился (нет самих документов; извлечение
      «по помещениям» — `element_key` — воркерами не поддержано).

### M4 — 25–26.09 · Протокол и верификация
- [x] У2: сборка протокола — **только JSON** (`GET /processes/{id}/protocol`), не 5 отдельных таблиц;
      `findings`/`decision` реализованы и проверены (включая обязательность `comment`/`reason_code`
      по backend-plan.md §7); `split` и `dataset_items` — **не реализованы**
- [x] У2: `finalize`/`unfinalize` (переход в `FINALIZED`, роль supervisor/admin для unfinalize, аудит)
- [ ] У1: экспорт PDF (gotenberg + миниатюры с bbox), XML — **не реализован**
- [x] У1: `cmd/rin-sync` + `cmd/rin-mock`, ретраи 1/5/15 мин (конфигурируемо), `PENDING_SYNC` →
      `SYNCED`/`SYNC_FAILED`; 4xx не ретраится, 5xx/таймаут — ретраится; `rin.Signer` — заглушка
      (`NoopSigner`, УКЭП не определена в ТЗ)
- [ ] У1: инкрементальный пересчёт (индекс параметр → источники, `input_hash`) — `input_hash` в
      `evidence_groups` считается, но не переиспользуется; полноценная инкрементальность — TODO
- [ ] У2: suspicions — **не реализованы** (нет ни приёма от воркера, ни `promote`)
- **DoD (адаптирован):** полный цикл «загрузка → протокол → решение по CANDIDATE → финализация →
      отправка в rin-mock → SYNCED» — **проверено вручную end-to-end на живом стенде** (1 решение,
      не 3 — в тестовом сценарии был один `CANDIDATE`); сценарий с неудачей и ретраем rin-sync
      проверен автоматическими тестами (`internal/rin`, не на живом стенде в этом прогоне).

### M5 — 27.09 · Feature freeze
- [ ] Типы правил `presence`, `tolerance`, `text_mismatch`; расширение набора `rules/*.yaml`
- [ ] Админ-эндпоинты Матрицы/нормативов, экспорт датасета, еженедельный отчёт (минимальный)
- [ ] Нагрузочный прогон: 500 страниц ≤ 10 мин, API p95 ≤ 200 мс (k6); фиксы
- **DoD:** `make e2e` зелёный на двух объектах

### M6 — 28.09 · Code freeze
- [ ] Чистая установка по README на другой машине, бэкап/восстановление Postgres
- [ ] Обновить `docs/arch.md` (включая обоснование Go и Kafka)
- [ ] Только исправление ошибок

---

## 12. Открытые вопросы (не блокируют работу, решить по ходу)

- Допустимы ли Go и Kafka вместо Node.js и RabbitMQ — вопрос модератору.
- Формат обмена с ИАИС «РиН» не опубликован — пока собственная JSON-схема в `contracts/rin.schema.json`.
- Коды параметров в примерах ТЗ (KR-55, AR-41) не совпадают с xlsx (M-055, M-041): канон — xlsx, alias_code для ТЗ.
- Поля листа «СХЕМА GOLD» — сверить с `dataset_items` при реализации M4.

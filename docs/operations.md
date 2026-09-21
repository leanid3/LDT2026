# Эксплуатация

Runbook для тех, кто держит стенд/прод в рабочем состоянии: что смотреть, куда идти при алерте, как
диагностировать типовые сбои, как бэкапить и восстанавливать данные. Как развернуть сервис — см.
[deployment.md](deployment.md); как оно устроено — [architecture.md](architecture.md).

## Health-checks

| Эндпоинт | Порт | Что проверяет | Ожидаемый ответ |
|---|---|---|---|
| `GET /healthz` | 8080 (api) | Liveness — процесс жив, зависимости не проверяются | `200 {"status":"ok"}`, всегда, пока процесс запущен |
| `GET /readyz` | 8080 (api) | Readiness — реальная проверка зависимостей (сейчас: ping Postgres) | `200 {"status":"ready"}` / `503 {"status":"not_ready","failed":{...}}` |
| `GET /metrics` | 9091 (api, отдельный порт) | Prometheus-метрики, namespace `inspector_` | `200`, `text/plain` |

`/healthz` используйте для перезапуска контейнера (Docker/K8s restart policy), `/readyz` — для
исключения из балансировки/готовности принимать трафик. Реализация — [internal/platform/health](../backend/internal/platform/health/health.go);
новые зависимости (Kafka, MinIO) добавляются в `/readyz` через `health.Checker` по мере их появления
в `cmd/api` (M1+) — на M0 там только `postgres`.

## Метрики (Prometheus, namespace `inspector_`)

Уже собираются (M0): `inspector_http_requests_total`, `inspector_http_request_duration_seconds` — через
`metrics.Middleware()` на каждый HTTP-запрос.

Запланированы по мере реализации доменов (backend-plan.md §10) — при добавлении кода, который их
эмитит, обновляйте эту таблицу:

| Метрика | Появится в | Смысл |
|---|---|---|
| `inspector_outbox_pending` | M1 | Размер очереди недоставленных outbox-событий — рост означает, что `relay` не успевает или упал |
| `inspector_kafka_consume_errors_total` | M1 | Ошибки в consumer-цикле |
| `inspector_dlq_messages_total` | M1 | Сообщения, ушедшие в `*.dlq` — любое значение > 0 требует разбора |
| `inspector_jobs_duration_seconds{kind}` | M2–M3 | Длительность `parse_jobs`/`extract_jobs` |
| `inspector_jobs_failed_total{kind}` | M2–M3 | Задания, исчерпавшие ретраи (по ТЗ — до 2 повторов, затем алерт администратору) |
| `inspector_engine_check_duration_seconds` | M3 | Время движка проверок на процесс (цель ТЗ 11: ≤ 2 мин на 132 параметра) |
| `inspector_findings_total{status}` | M3 | Счётчик findings по статусам |
| `inspector_rin_sync_total{result}` | M4 | Результаты отправки в ИАИС «РиН» |

Grafana: `http://<host>:3001` (dev: `admin`/`admin`, анонимный доступ включён локально —
**отключите `GF_AUTH_ANONYMOUS_ENABLED` на стенде за пределами локальной машины**, сейчас это удобство
для разработки, не для сети общего доступа). Datasource на Prometheus провижинится автоматически из
`infra/grafana/provisioning/datasources/prometheus.yml`.

## Алерты (Alertmanager)

Пороги по backend-plan.md §10 (реализуются в `infra/prometheus/` правилами алертов по мере появления
метрик выше — на M0 заведён только сам Alertmanager с пустым stub-конфигом
`infra/alertmanager/alertmanager.yml`):

| Условие | Что делать |
|---|---|
| CPU > 80% | Проверить, какой процесс — если `engine`, возможно застряли задания или бесконечный ретрай |
| p95 HTTP-запросов > 500 мс | Смотреть `inspector_http_request_duration_seconds` по хендлерам, проверить БД (медленные запросы, недостаток индексов) |
| DLQ > 0 в течение 5 мин | См. раздел [DLQ](#разбор-dlq) ниже — не игнорировать, это потерянные/незавершённые события |
| `outbox_pending` > 100 в течение 5 мин | `relay` не успевает или упал — проверить логи `relay`, число реплик, доступность Kafka |
| `jobs_failed_total` растёт | Задания парсинга/извлечения не проходят — проверить Python-воркеры, `contracts/facts.schema.json`/`layout.schema.json` на рассинхронизацию контракта |
| `rin_sync_total{result="failed"}` / `sync_status=SYNC_FAILED` | ИАИС «РиН» недоступна или отклоняет запрос — см. логи `rin-sync`, при необходимости `rin-mock` для локальной проверки ретраев |

Каналы уведомлений (email + Telegram) — по backend-plan.md, не настроены на M0, конфигурируются в
`infra/alertmanager/alertmanager.yml` при подключении реального стенда.

## Логи

`log/slog`, JSON в stdout (не файлы — файловый режим был в исходном каркасе и осознанно убран, см.
[architecture.md](architecture.md)). Поля: `timestamp, level, service, message`, плюс `request_id` на
каждый HTTP-запрос (добавляется `middleware.Logger`) и `user_id`/`process_id` там, где применимо в
доменном коде.

```bash
# локально
docker compose -f infra/docker-compose.yml logs -f <service>

# сборка по request_id (сквозной поиск конкретного запроса)
docker compose logs api | jq -c 'select(.request_id == "<uuid>")'
```

На стенде без ELK/Loki (M0–M2) — `docker logs`/`journalctl` по контейнеру. Подключение в
Elasticsearch/Loki — задача M3 (backend-plan.md, «логи в ELK/Loki»); когда появится — обновить этот
раздел ссылкой на дашборд/индекс.

## Kafka

`kafka-ui` (`http://<host>:8090`) — первое место для диагностики:

- **Topics** → размер накопленных сообщений на партицию — растёт, если consumer отстаёт или не запущен.
- **Consumer groups** → лаг по группе — покажет, какой сервис не успевает или упал.
- **`*.dlq`** топики → любое сообщение здесь требует разбора (см. ниже).

### Разбор DLQ

Сообщение в `<topic>.dlq` означает: consumer не смог обработать событие за 3 попытки с backoff.
Каждое сообщение в DLQ несёт исходное событие + ошибку (backend-plan.md §6.3). Порядок разбора:

1. Прочитать сообщение в kafka-ui, понять ошибку (обычно: невалидные данные от воркера, рассинхрон
   контракта `contracts/facts.schema.json`, временная недоступность БД).
2. Если ошибка временная (была недоступна БД/сеть) и уже устранена — событие можно вручную переиграть
   (republish в исходный топик; ручной инструмент под это — TODO, пока через `kafka-console-producer`
   или API kafka-ui).
3. Если ошибка систематическая (баг в коде/контракте) — сначала фикс, потом replay, иначе событие снова
   уйдёт в DLQ.
4. `dlq_messages_total` не должен расти в фоне без разбора — это прямой сигнал алерта.

## Бэкапы

Постоянные данные: Postgres (`pgdata` volume) и MinIO (`miniodata` volume). Kafka (`kafkadata`) — это
event log с retention 7/30 дней, не источник истины для бэкапа (при потере переигрывать нечего, но и
не критично — outbox/consumed_events переживают в Postgres).

```bash
# бэкап Postgres (по backend-plan.md §10: ежедневно, cron-контейнер, хранение 30 дней)
docker compose -f infra/docker-compose.yml exec postgres \
    pg_dump -U postgres -Fc inspector > backup_$(date +%F).dump

# восстановление на чистую БД
docker compose -f infra/docker-compose.yml exec -T postgres \
    pg_restore -U postgres -d inspector --clean --if-exists < backup_2026-09-21.dump
```

Автоматизация ежедневного `pg_dump` в cron-контейнере с ротацией 30 дней — задача M6 (чистая установка
и бэкап/восстановление проверяются как часть code freeze checklist, backend-plan.md).

MinIO: бэкапить бакет `documents` `mc mirror` на внешнее хранилище, либо полагаться на volume-снапшоты
инфраструктуры хостинга — конкретный способ зависит от того, где именно развёрнут стенд (см.
[deployment.md](deployment.md)).

## Частые сценарии

| Симптом | Куда смотреть |
|---|---|
| `/readyz` отдаёт 503 | Тело ответа перечисляет, какая зависимость упала (`failed: {...}`) — начать с неё |
| Инспектор не видит новый файл в протоколе | kafka-ui: дошло ли `doc.parse.requested`/`doc.extract.completed`, лаг consumer group `engine` |
| Долго не финализируется процесс | Проверить, есть ли `finding` с `finding_status=CANDIDATE` и `inspector_status=PENDING` — финализация блокируется, пока такие есть (lifecycle.md#процесс-проверки) |
| В `rin_sync` `SYNC_FAILED` | Логи `rin-sync`, доступность целевой ИАИС «РиН» (или `rin-mock` при демо), проверить, не исчерпаны ли ретраи (1/5/15 мин) |
| Рост `outbox_events` со `status=failed` | Kafka недоступна/сеть — проверить `kafka` healthcheck, затем логи `relay` |
| Подозрение на утечку секрета в логах | Grep по `password|secret|token` в логах — по правилу проекта пароли/коннекшн-строки в лог не пишутся; если найдено — это баг, заводить как P1 |

## Безопасность (эксплуатационный минимум)

- TLS терминируется на Caddy перед API (TLS 1.3, backend-plan.md §10) — не открывайте `api:8080`
  наружу напрямую, только через reverse proxy.
- ClamAV проверяет каждый загружаемый файл при `confirm` — если ClamAV недоступен, загрузка файлов
  должна отказывать, а не пропускать непроверенные файлы (fail-closed; проверить при реализации M1).
- Секреты — только ENV/секрет-хранилище, никогда в `config.yaml`/git (см. `.gitignore`, правило №10).
- `audit_log` — источник истины для «кто и что сделал»; при разборе инцидента с решениями по findings
  начинайте оттуда (backend-plan.md §12.4 — поля аудита).

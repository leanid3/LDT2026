# CLAUDE.md — backend (Go)

Сервис «Инспектор ИИ»: сверка ПД / РД / ИД по Матрице из 132 параметров для Мосгосстройнадзора.
Хакатон ЛЦТ 2026, кейс 10. Разработка до 29.09.2026, code freeze вечером 28.09.

Полный план работ, модель данных, контракты и порядок задач: `../docs/backend-plan.md`.
Прочитай его целиком перед первой задачей. Исходные документы заказчика лежат в `../docs/source/`.

## Стек (не менять без согласования с человеком)

- Go 1.24, один модуль `backend/`, несколько бинарей в `cmd/`.
- HTTP: gin. Контракт spec-first: `../contracts/openapi.yaml` (OpenAPI 3.0) → oapi-codegen (gin-server + types).
- БД: PostgreSQL, pgx/v5, миграции golang-migrate в `migrations/`.
- Брокер: Kafka, клиент confluent-kafka-go/v2 (cgo, librdkafka).
- Хранилище: MinIO (minio-go/v7).
- Конфиг: cleanenv (YAML + переопределение через ENV). Секреты только через ENV.
- Логи: `log/slog`, JSON в stdout. Метрики: prometheus/client_golang.
- Тесты: testify + testcontainers-go (postgres, kafka, minio).

## Правила работы

1. Контракт первым. Любое изменение API: сначала `contracts/openapi.yaml`, затем `make gen`, затем код.
   Любое изменение событий Kafka: сначала JSON Schema в `contracts/events/`.
   Контракты общие с Python-воркерами и фронтендом — не ломай обратную совместимость молча, пиши в PR.
2. Миграции только добавляются. Не редактируй уже применённую миграцию, создай новую.
3. Статусы и enum-значения — строго как в `docs/backend-plan.md` (они из ТЗ заказчика). Не придумывай новые названия.
4. Kafka-consumer: `enable.auto.commit=false`. Commit offset только после COMMIT транзакции в Postgres.
   Каждый consumer идемпотентен через таблицу `consumed_events`.
5. Публикация в Kafka из бизнес-кода — только через outbox (INSERT в `outbox_events` в той же транзакции).
   Напрямую в Kafka пишет только `cmd/relay`.
6. `CONFIRMED_VIOLATION` может поставить только человек (инспектор/супервизор) через API верификации.
   Автоматика никогда не присваивает этот статус.
7. После `FINALIZED` любые изменения протокола и дозагрузка запрещены (409), кроме отмены финализации супервизором/админом.
8. Не трогай `../workers/` и `../frontend/`. Если нужна правка на их стороне — опиши её в PR/комментарии.
9. Каждая задача заканчивается: `make lint test` зелёные, миграции применяются на чистой БД, обновлён `openapi.yaml` при изменении API.
10. Не коммить секреты, `config.yaml`, `.env`. Только `*.example`.
11. Если требование ТЗ неоднозначно — не додумывай молча: сделай минимально безопасный вариант и оставь `// TODO(TZ): ...` с вопросом.

## Команды

```bash
make up          # docker compose up -d (вся инфраструктура)
make gen         # oapi-codegen из contracts/openapi.yaml
make migrate     # применить миграции
make seed        # импорт Матрицы из docs/source/*.xlsx + тестовые пользователи
make run-api     # локальный запуск API
make lint test   # golangci-lint + go test ./... (интеграционные через testcontainers)
make e2e         # сквозной сценарий на тестовом объекте
```

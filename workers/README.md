# workers — Python-воркеры «Инспектора ИИ»

Два воркера, которые стоят между Go-оркестратором (`backend/`) и Kafka-топиками обработки документов.
Контракт — только `contracts/events/*.schema.json` и `contracts/facts.schema.json`.

```
doc.parse.requested  ─►  parse-воркер    ─►  layout в MinIO  ─►  doc.parse.completed
doc.extract.requested ─► extract-воркер  ─►  facts.jsonl в MinIO ─► doc.extract.completed
```

| Воркер | Что делает |
|---|---|
| **parse** | Скачивает файл из MinIO, определяет формат по содержимому (PDF / DOCX / XML), строит layout: страницы → строки с нормализованным `bbox` [0;1]. Страницы PDF без текстового слоя → OCR (если есть tesseract), иначе честная оценка качества `LOW_QUALITY`/`ABSTAIN`. |
| **extract** | По layout'ам находит значения параметров Матрицы (`contracts/matrix.json`): шаблоны `regex` (44 параметра) + опционально LLM (`LLM_BASE_URL`, `LLM_MODEL`). Приводит значения к единицам Матрицы (`800 мм → 0.8 м`), канонизирует марки (`В30 → B30`), пишет JSONL фактов. |

## Гарантии
- Идемпотентность: `consumed_events` + публикация только через `outbox_events` в одной транзакции (как Go-сервисы). Offset коммитится после COMMIT.
- Сбой ≠ зависший процесс: после `WORKER_MAX_ATTEMPTS` воркер сам публикует `FAILED`-результат и копию события в `<topic>.dlq`.
- Доказательность: у каждого факта есть `quote` и `bbox`. LLM-факты принимаются, только если значение дословно есть в указанной строке (иначе — выдумка, отбрасывается).
- Свои исходящие события и факты валидируются по тем же JSON Schema (`WORKER_VALIDATE_EVENTS=true`).

## Запуск
```bash
cd workers && make venv
make run-parse       # в одном терминале
make run-extract     # в другом
```
Настройки — ENV (те же имена, что у backend): `DATABASE_*`, `BROKER_BOOTSTRAP_SERVERS`, `MINIO_*`; свои: `LLM_BASE_URL`, `LLM_MODEL`, `LLM_API_KEY`, `WORKER_MAX_ATTEMPTS`, `WORKER_OCR`, `CONTRACTS_DIR`.

> **Не запускайте вместе с `backend/cmd/mockworkers`** — оба слушают одни и те же топики и ответили бы дважды.

## Тесты
`make test` — юнит-тесты (нужен TTF-шрифт с кириллицей для генерации PDF; `TEST_FONT=/path/to.ttf`).
`tests/test_bus_integration.py` — против настоящего Postgres, включается переменной `TEST_DATABASE_DSN`.

## Границы
- OCR не проверен на реальном tesseract в этой среде (его тут нет) — код гардится `ocr.available()`.
- Нет `set_difference`/`presence`/`tolerance`-извлечения и подозрений (suspicions) — см. docs/architecture.md.
- Извлечение: только «явные» формулировки; остальное — на LLM или честный `NOT_COMPARABLE` в движке.

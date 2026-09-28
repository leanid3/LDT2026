# API для фронтенда

Всё, что нужно, чтобы начать делать интерфейс. Источник истины — [`contracts/openapi.yaml`](../contracts/openapi.yaml)
(OpenAPI 3.0, версия 0.3.0); этот документ — навигатор по нему: что за чем вызывать и что показывать.

## Быстрый старт

| Что | Где |
|---|---|
| **Swagger UI** (можно пробовать запросы, кнопка *Authorize* принимает JWT) | `http://localhost:8080/swagger` |
| Сама спецификация | `http://localhost:8080/contracts/openapi.yaml` или файл `contracts/openapi.yaml` |
| Базовый URL API | `http://localhost:8080/api/v1` |
| CORS | разрешены `http://localhost:5173`, `http://127.0.0.1:5173`, `http://localhost:3000`; свои — `SERVER_CORS_ORIGINS` |

Типы для TypeScript генерируются прямо из контракта:

```bash
npx openapi-typescript ../contracts/openapi.yaml -o src/shared/api/schema.d.ts
```

Демо-данные: `make seed` (в `backend/`) создаёт четырёх пользователей, пароль у всех `demo-password-123`:
`inspector1`, `supervisor1`, `admin1`, `ml1`. Стенд и сервисы — [development.md](development.md#полный-конвейер-локально);
прогнать весь сценарий одной командой и получить готовые данные для интерфейса: `python3 scripts/e2e.py`.

## Общие правила

- **Авторизация**: `POST /auth/login {login, password}` → `{access_token, expires_in}` (12 ч). Дальше везде
  заголовок `Authorization: Bearer <token>`. Refresh-токенов нет — по истечении (401) просто логин заново.
  Роль — `GET /auth/me` (`inspector` | `supervisor` | `admin` | `ml_engineer`); от неё зависят кнопки
  (например, «Отменить финализацию» — только `supervisor`/`admin`, иначе 403).
- **Формат ошибок** — единый:
  ```json
  { "error": { "code": "CONFLICT", "message": "…по-русски, можно показывать…", "details": {} }, "request_id": "…" }
  ```
  Коды: `VALIDATION_FAILED` (400), `UNAUTHORIZED` (401), `FORBIDDEN` (403), `NOT_FOUND` (404),
  `CONFLICT` (409 — действие недопустимо в текущем статусе), `INTERNAL_ERROR` (500). `request_id` полезно
  показывать в «Сообщить о проблеме».
- **Асинхронность**: всё тяжёлое (разбор документов, извлечение, сравнение) идёт в фоне. Запуск
  проверки отвечает сразу, о прогрессе узнаём опросом `GET /processes/{id}` (раз в 2 с достаточно).
  WebSocket/SSE **нет**.
- Все id — UUID, даты — RFC 3339 (UTC).

## Экраны → эндпоинты

### 1. Объекты
| Действие | Запрос |
|---|---|
| Список / карточка | `GET /objects`, `GET /objects/{id}` |
| Создать | `POST /objects {name, address?, customer?, contractor?, permit_number?}` (`name` обязателен) |
| Проверки объекта | `GET /objects/{id}/processes` — новые сверху, со статусом и сценарием |

### 2. Загрузка документов (мастер из 4 шагов)

1. **Заявить файлы** — `POST /documents/upload {object_id, files:[{original_name, size_bytes}]}`
   (без `process_id` создаётся новая проверка; с ним — дозагрузка). Лимиты: 50 МБ на файл, 200 МБ на
   пакет, форматы PDF/DOCX/XML. В ответе для каждого файла `file_id`, `upload_url`, `upload_fields`.
2. **Загрузить в хранилище** — прямой `multipart/form-data` POST на `upload_url` (браузер → MinIO, минуя API):
   **сначала все `upload_fields`, файл — последним полем `file`**. Ссылка живёт 30 минут.
   ```ts
   const form = new FormData();
   Object.entries(slot.upload_fields).forEach(([k, v]) => form.append(k, v));
   form.append("file", file);                       // строго последним
   await fetch(slot.upload_url, { method: "POST", body: form });
   ```
3. **Подтвердить** — `POST /documents/{process_id}/confirm` → по каждому файлу `check_status`
   (`ACCEPTED` | `REJECTED_FORMAT` | `REJECTED_SIZE` | `REJECTED_CORRUPTED` | `REJECTED_VIRUS`),
   `check_error`, `page_count`. Отклонённые показать пользователю, остальные идут дальше.
4. **Реестр** — `POST /documents/{process_id}/registry` (multipart, поле `file`; CSV/JSON/XLSX): задаёт
   для каждого файла стадию **ПД/РД/ИД**, шифр, редакцию, статус утверждения. **Без реестра у файлов нет
   стадии, и проверка ничего не найдёт.** Ответ: `matched`/`unmatched`/`errors`. Колонки CSV:
   `object_id,file_name,sha256,doc_stage,discipline,document_code,revision,approval_status,approval_date,sheet_page_range,predecessor_ref,successor_ref,signature_status`.
   После реестра `GET /processes/{id}` покажет `scenario` (какие стадии есть) и `upload_status` по стадиям;
   `GET /processes/{id}/files` — какие редакции признаны актуальными (`is_current`, `selection_status`).

### 3. Запуск и ожидание
`POST /processes/{id}/start` → опрос `GET /processes/{id}` до `status: READY`.
Статусы процесса: `PENDING → PARSING → READY → VERIFYING → COMPLETED → FINALIZED`.

### 4. Протокол и верификация (главный экран инспектора)

`GET /processes/{id}/protocol` → `findings[]`, уже отсортированы для работы: сначала `CANDIDATE`
(требуют решения), внутри — по приоритету параметра (HIGH выше). У каждого finding:

| Поле | Что показать |
|---|---|
| `param_code`, `parameter_name`, `unit`, `review_priority` | заголовок карточки: «M-055 · Класс прочности бетона · HIGH» |
| `expected_value` → `actual_value`, `delta` | «B35 → B30» (значения приведены к единице параметра) |
| `rationale` | пояснение системы человеческим языком |
| `finding_status` | вердикт системы (см. ниже) |
| `inspector_status`, `reason_code`, `comment`, `decided_by/at` | решение инспектора |
| `evidence[]` | доказательства обеих сторон — см. просмотрщик |

**Статусы finding** (`finding_status`): `CANDIDATE` — расхождение, нужно решение человека;
`NEGATIVE_VERIFIED` — значения совпадают; `MISSING_EVIDENCE` — не хватает значения в одной из сторон;
`NOT_COMPARABLE` / `NOT_APPLICABLE` — сравнить нельзя / неприменимо; `CONFIRMED_VIOLATION` — нарушение
подтверждено **человеком** (система его не ставит никогда); `CLARIFICATION_REQUIRED`, `SUSPICION`.

**Решение**: `POST /findings/{id}/decision {decision, reason_code?, comment?}`:
- `CONFIRMED_VIOLATION` — обязателен `comment`;
- `NEGATIVE_VERIFIED` (отклонить как ложное) — обязательны `reason_code` (`WRONG_REVISION`, `APPROVED_CHANGE`,
  `OCR_ERROR`, `LINKING_ERROR`, `NOT_APPLICABLE`, `DUPLICATE`, `OTHER`) и `comment`;
- `CLARIFICATION_REQUIRED` — запросить уточнение.
Ответ — обновлённая карточка. Первое решение переводит процесс в `VERIFYING`, когда решены все
`CANDIDATE` — в `COMPLETED`. Ошибка 400 — не хватает обязательных полей (`error.message` объясняет).

### 5. Просмотрщик «рядом» (ПД ↔ РД) с подсветкой

Для каждого `evidence[i]`: `file_id`, `original_name`, `stage` (PD/RD/ID), `page` (с 1), `quote`,
`extracted_value`, `role` (`expected` — эталон, `actual` — факт) и **`bbox`**.

- `GET /files/{file_id}/download-url` → `{url, expires_at}` — временная ссылка (15 мин) на сам файл; PDF
  открывается в `pdf.js`/`<iframe>` на нужной странице (`#page=N`). Запрашивайте ссылку при открытии
  карточки, не кешируйте дольше `expires_at`.
- **`bbox = [x0, y0, x1, y1]`, нормализованно в [0;1], начало координат — левый верхний угол страницы.**
  Подсветка поверх страницы — просто проценты:
  ```css
  .hl { position:absolute;
        left: calc(var(--x0) * 100%);  top: calc(var(--y0) * 100%);
        width: calc((var(--x1) - var(--x0)) * 100%);  height: calc((var(--y1) - var(--y0)) * 100%); }
  ```
- `bbox` **отсутствует**, если у источника нет геометрии; `[0,0,1,1]` — положение на странице неизвестно
  (DOCX/XML) — подсвечивать всю страницу или не подсвечивать, показывать `quote`.
- `quote` — дословная цитата из документа: показывайте её рядом с подсветкой (это и есть доказательство).

### 6. Финализация и передача в ИАИС «РиН»
- `POST /processes/{id}/finalize` — только из `COMPLETED` и если нет нерешённых `CANDIDATE` (иначе 409).
  После — `FINALIZED`: изменения и дозагрузка запрещены (409).
- `GET /processes/{id}/sync` → `sync_status`: `PENDING_SYNC → SYNCED` | `SYNC_FAILED` (опрос раз в несколько секунд).
- `POST /processes/{id}/unfinalize {reason}` — отмена, только `supervisor`/`admin`.

### 7. Справочники
`GET /params` — каталог 132 параметров Матрицы (код, раздел, название, единица, приоритет, тип данных,
источники в ПД/РД/ИД, логика). Для фильтров и подписей; грузить один раз.

## Чего в API пока нет

WebSocket/SSE-уведомлений (только опрос); экспорта протокола в PDF/XML/DOCX (только JSON); разбиения
finding (`split`), списка подозрений, админ-эндпоинтов Матрицы и пользователей; refresh-токенов; поиска и
пагинации в списках (списки небольшие). Подробнее — [architecture.md](architecture.md#границы-текущей-реализации).
Если чего-то не хватает для экрана — сначала правка `contracts/openapi.yaml` (spec-first), затем код.

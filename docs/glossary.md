# Глоссарий

Термины и статусы проекта. Все enum-значения — канонические из ТЗ заказчика, закреплены в
[backend-plan.md §4.1](backend-plan.md#41-enum-значения-строго-как-в-тз); правило проекта — не
придумывать новые названия и не переводить их (правило №3, `backend/CLAUDE.md`).

## Документы и стадии

- **ПД (PD)** — проектная документация.
- **РД (RD)** — рабочая документация.
- **ИД (ID)** — исполнительная документация.
- **Матрица параметров** — таблица из 132 нормативных параметров (лист «МАТРИЦА» в
  `Матрица_параметров_редакция1_1.xlsx`), для каждого — источник в ПД/РД/ИД, правило сравнения, ссылки
  на нормативы. Импортируется в таблицу `params` (`cmd/tools/import-matrix`, M2).
- **Реестр (файлов)** — обязательный перечень документов по объекту (`Перечень_исполнительной_документации...docx`),
  без которого файлы получают `CLARIFICATION_REQUIRED`.
- **Редакция (revision)** — версия документа; для сравнения используется только актуальная утверждённая
  редакция (`approval_status`, выбор — backend-plan.md §8.2).

## Статусы объекта и процесса

- **`object`** — объект строительства, к которому привязаны процессы проверки.
- **`process`** — один прогон проверки комплекта документов по объекту.
  `process_status`: `PENDING → PARSING → READY → VERIFYING → COMPLETED → FINALIZED` —
  подробности и диаграмма в [lifecycle.md#процесс-проверки](lifecycle.md#процесс-проверки-process_status).
- **`scenario`** — какие стадии документации есть в процессе: `FULL` (ПД+РД+ИД), `PD_RD_ONLY`,
  `PD_ID_ONLY`, `RD_ID_ONLY`, `SINGLE_ONLY`, `PARTIALLY_LOADED` (хотя бы одна стадия неполная).
- **`upload_status`** — статус загрузки по стадии: `{PD,RD,ID}_{UPLOADED,PARTIAL,MISSING}`.

## Файлы

- **`file_check_status`** — `UPLOADING → UPLOADED → ACCEPTED` либо `REJECTED_{FORMAT,SIZE,CORRUPTED,VIRUS}`.
- **`approval_status`** — `DRAFT | APPROVED | FOR_CONSTRUCTION | SUPERSEDED | CANCELLED`. Эталоном для
  сравнения может быть только `APPROVED`/`FOR_CONSTRUCTION` без более нового утверждённого потомка;
  `DRAFT` эталоном быть не может.
- **`selection_status`** — итог алгоритма выбора актуальной редакции: `is_current=true` либо
  `CLARIFICATION_REQUIRED` с причиной.

## Проверка и доказательства

- **`evidence_group`** — единица протокола: объект + атомарный параметр/правило (+ `element_key`,
  например номер помещения) + набор актуальных редакций + доказательные фрагменты.
- **`finding`** — результат проверки evidence group: статус + карточка доказательства. Составной
  `finding` инспектор может разбить на атомарные (`split`, ссылка через `parent_finding_id`).
- **`finding_status`** (ставит движок проверок): `CANDIDATE` (расхождение найдено), `NEGATIVE_VERIFIED`
  (расхождения нет), `MISSING_EVIDENCE` (обязательный источник отсутствует), `NOT_APPLICABLE` (параметр
  неприменим), `NOT_COMPARABLE` (нет правила или качественных фактов), `CLARIFICATION_REQUIRED`
  (источник из неоднозначной группы редакций), `SUSPICION` (свободный поиск, не нарушение).
- **`inspector_status`** (ставит человек): `PENDING → CONFIRMED_VIOLATION | NEGATIVE_VERIFIED | CLARIFICATION_REQUIRED`.
  **Только человек может поставить `CONFIRMED_VIOLATION`** — движок никогда (правило №6, `backend/CLAUDE.md`).
- **`review_priority`** — `HIGH | MEDIUM | LOW`, только очерёдность проверки, не юридическое значение.
- **`reject_reason_code`** — причина отклонения кандидата: `WRONG_REVISION | APPROVED_CHANGE | OCR_ERROR |
  LINKING_ERROR | NOT_APPLICABLE | DUPLICATE | OTHER`.
- **`bbox`** — координаты доказательства на странице, нормализованные в `[0;1]` с учётом
  CropBox/MediaBox/поворота страницы.
- **`suspicion`** — гипотеза о нарушении, найденная свободным поиском (не по конкретному параметру
  Матрицы), не является нарушением, пока инспектор не выполнит `promote` в `CANDIDATE` с доказательствами.

## Протокол

- **`protocol`** — итоговый документ проверки: версия, 5 таблиц (комплектность/сопоставимость,
  кандидаты, подтверждённые нарушения, проверенные отрицательные результаты, гипотезы suspicion),
  версии Матрицы/датасета/модели, хеш входного манифеста.
- **`protocol_status`** — `DRAFT → VERIFICATION_COMPLETED → PROTOCOL_FINALIZED`.
- **GOLD** — размеченный датасет для дообучения модели: `CONFIRMED_VIOLATION` и `NEGATIVE_VERIFIED` с
  полной карточкой доказательства, разбитый по `object_id` (`dataset_items`, экспорт — `cmd/tools/export-dataset`).

## Интеграция

- **ИАИС «РиН»** — внешняя информационная система, куда передаются финализированные протоколы.
- **`sync_status`** — `NOT_REQUIRED | PENDING_SYNC | SYNCED | SYNC_FAILED`.

## Роли

`inspector` (загрузка, просмотр, решения, финализация), `supervisor` (+ отмена финализации), `admin`
(+ Матрица, нормативы, пользователи), `ml_engineer` (чтение, экспорт датасета, отчёты).

## Инфраструктурные термины

- **Outbox / Relay** — паттерн транзакционной доставки событий в Kafka; см.
  [architecture.md#transactional-outbox--relay](architecture.md#transactional-outbox--relay).
- **DLQ (dead-letter queue)** — топик `<topic>.dlq`, куда уходит событие после исчерпания попыток
  обработки; см. [operations.md#разбор-dlq](operations.md#разбор-dlq).
- **Unit of work / `WithTx`** — обёртка над транзакцией Postgres, гарантирующая, что бизнес-изменение и
  outbox-запись коммитятся вместе; см. [architecture.md#unit-of-work-dbwithtx](architecture.md#unit-of-work-dbwithtx).
- **KRaft** — режим работы Kafka без ZooKeeper (метаданные кластера хранятся самими брокерами).

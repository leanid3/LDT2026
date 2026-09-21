# Инспектор ИИ

Сервис автоматизированной сверки исполнительной документации (ПД / РД / ИД) со 132‑параметрической
Матрицей нормативных требований для **Мосгосстройнадзора**. Хакатон ЛЦТ 2026, кейс 10.

Инспектор загружает комплект документов по объекту → сервис асинхронно разбирает файлы, сравнивает
параметры между проектной, рабочей и исполнительной документацией по правилам Матрицы → инспектор
подтверждает или отклоняет кандидаты в нарушения в веб‑интерфейсе → формируется протокол проверки,
который при финализации уходит в ИАИС «РиН».

> Статус: активная разработка, hackathon-темп (дедлайн 29.09.2026, code freeze 28.09.2026 вечером).
> Веха **M0 (каркас и стенд) выполнена**, актуальный статус — [docs/backend-plan.md §11](docs/backend-plan.md#11-порядок-задач).

## Документация

| Документ | О чём |
|---|---|
| [docs/architecture.md](docs/architecture.md) | Архитектура, компоненты, паттерны, архитектурные решения |
| [docs/lifecycle.md](docs/lifecycle.md) | Жизненные циклы: процесс проверки, файл, finding, событие, HTTP-запрос, сервис |
| [docs/development.md](docs/development.md) | Локальная разработка: сборка, запуск, тесты, генерация кода |
| [docs/deployment.md](docs/deployment.md) | Сборка и развёртывание на стенде |
| [docs/operations.md](docs/operations.md) | Эксплуатация: health-checks, метрики, логи, алерты, бэкапы, troubleshooting |
| [docs/glossary.md](docs/glossary.md) | Глоссарий доменных терминов и статусов |
| [docs/backend-plan.md](docs/backend-plan.md) | Полный план backend-части: требования ТЗ, модель данных, контракты, вехи |
| [backend/CLAUDE.md](backend/CLAUDE.md) | Технические соглашения и правила работы над `backend/` |

Начать стоит с [docs/development.md](docs/development.md), если нужно просто поднять стенд и что-то
пощупать, и с [docs/architecture.md](docs/architecture.md), если нужно разобраться, как это устроено.

## Структура репозитория

```
.
├── backend/            # Go-бэкенд: cmd/{api,relay,engine,rin-sync,rin-mock,tools/*}, internal/*
├── contracts/          # Общие контракты: OpenAPI 3.0, JSON Schema событий Kafka
├── infra/              # docker-compose стенд: postgres, kafka, minio, prometheus, grafana, caddy, ...
├── tools/              # Отдельный Go-модуль для dev-инструментов (golangci-lint, oapi-codegen)
├── docs/               # Документация (этот раздел)
├── workers/            # Python: парсинг документов и LLM-извлечение фактов (участники 4, 5)
└── frontend/           # React (участник 3)
```

`backend/` и `tools/` — два независимых Go-модуля; почему так и что это значит на практике —
см. [docs/architecture.md#tools-модуль](docs/architecture.md#tools-модуль-и-почему-он-отдельный).

## Быстрый старт

```bash
git clone <repo> && cd Hakaton-LDT/backend
cp config.example.yaml config.yaml
make up            # поднять инфраструктуру (postgres, kafka, minio, ...)
make migrate       # применить миграции (черновая схема backend-plan.md §5, см. docs/development.md#миграции)
make run-api       # локальный запуск API
curl localhost:8080/healthz
```

Подробности, диагностика проблем и полный список команд — в [docs/development.md](docs/development.md).

## Технологический стек

| Слой | Технология |
|---|---|
| Backend | Go 1.24, gin, pgx/v5, confluent-kafka-go/v2, minio-go/v7 |
| API-контракт | OpenAPI 3.0, spec-first, `oapi-codegen` (gin-server) |
| БД | PostgreSQL 16, `golang-migrate` |
| Брокер событий | Apache Kafka (KRaft, без ZooKeeper) |
| Хранилище файлов | MinIO (S3-совместимое) |
| Антивирус / рендер PDF | ClamAV, Gotenberg |
| Наблюдаемость | `log/slog` (JSON), Prometheus, Grafana, Alertmanager |
| Reverse proxy | Caddy (TLS) |
| CI | GitHub Actions ([.github/workflows/ci.yml](.github/workflows/ci.yml)) |

Осознанные отступления от ТЗ (Go вместо Node.js, Kafka вместо RabbitMQ) и их обоснование — в
[docs/architecture.md#архитектурные-решения](docs/architecture.md#архитектурные-решения).

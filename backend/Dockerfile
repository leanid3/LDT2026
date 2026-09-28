# Один образ, все бинари backend (backend-plan.md §3: "Все бинари собираются в один образ"); конкретный
# сервис выбирается командой в docker-compose.
#
# Контекст сборки — КОРЕНЬ репозитория (нужны contracts/ и docs/source/*.xlsx):
#   docker build -f backend/Dockerfile -t inspector-backend:<tag> .
FROM golang:1.24-trixie AS builder

# confluent-kafka-go линкуется статически со встроенной librdkafka (CGO_ENABLED=1), системный
# librdkafka-dev не нужен — нужны только gcc/g++ для cgo.
RUN apt-get update && apt-get install -y --no-install-recommends gcc g++ \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .

RUN mkdir -p /out && \
    for cmd in api relay engine mockworkers rin-sync rin-mock; do \
      CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /out/$cmd ./cmd/$cmd; \
    done && \
    for tool in import-matrix seed-users; do \
      CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /out/$tool ./cmd/tools/$tool; \
    done

FROM debian:trixie-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Рабочий каталог /app/backend: относительные пути сервисов (rules, ../contracts/openapi.yaml,
# ../docs/source/*.xlsx) те же, что при `go run` из backend/ — раскладка каталогов повторяет репозиторий.
WORKDIR /app/backend
COPY --from=builder /out/ /app/bin/
COPY backend/config.example.yaml /app/backend/config.yaml
COPY backend/rules /app/backend/rules
COPY contracts /app/contracts
COPY docs/source/*.xlsx /app/docs/source/

RUN useradd -m -u 1000 appuser
USER appuser

# Пример: docker run ... image /app/bin/api
ENTRYPOINT []
CMD ["/app/bin/api"]

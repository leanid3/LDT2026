# Один образ, все бинари cmd/*; конкретный сервис выбирается командой в docker-compose
# (backend-plan.md §3: "Все бинари собираются в один образ").
FROM golang:1.24-trixie AS builder

# confluent-kafka-go линкуется статически со встроенной librdkafka (CGO_ENABLED=1), системный
# librdkafka-dev не нужен — нужны только gcc/g++ для cgo.
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    g++ \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN mkdir -p /build/bin && \
    for cmd in api relay engine rin-sync rin-mock; do \
      CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /build/bin/$cmd ./cmd/$cmd; \
    done

FROM debian:trixie-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /build/bin/ /app/bin/
COPY config.example.yaml /app/config.yaml

RUN useradd -m -u 1000 appuser
USER appuser

# Пример: docker run ... image /app/bin/api
ENTRYPOINT []
CMD ["/app/bin/api"]

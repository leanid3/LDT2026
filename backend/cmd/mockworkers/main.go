// cmd/mockworkers — НАША заглушка вместо Python parse-worker/LLM-worker (см. internal/mockworkers,
// docs/architecture.md). Только для локальной разработки/демо, не продакшен-сервис.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/config"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/mockworkers"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/kafka"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/logging"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/minio"
)

func main() {
	log := logging.New("info", "mockworkers")

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	log = logging.New(cfg.Logger.Level, "mockworkers")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	database, err := db.New(ctx, db.Config{
		Host: cfg.Database.Host, Port: cfg.Database.Port, User: cfg.Database.User,
		Password: cfg.Database.Password, Database: cfg.Database.Database,
		MaxConnections: cfg.Database.MaxConnections, MinConnections: cfg.Database.MinConnections,
		MaxConnLifetime: cfg.Database.MaxConnLifetime, MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
	}, log)
	if err != nil {
		log.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	minioClient, err := minio.New(ctx, minio.Config{
		Endpoint: cfg.Minio.Endpoint, AccessKey: cfg.Minio.AccessKey, SecretKey: cfg.Minio.SecretKey,
		Bucket: cfg.Minio.Bucket, UseSSL: cfg.Minio.UseSSL, Region: cfg.Minio.Region, Timeout: cfg.Minio.Timeout,
	}, log)
	if err != nil {
		log.Error("failed to connect to minio", "error", err)
		os.Exit(1)
	}

	handler := mockworkers.NewHandler(database, minioClient, log)

	dlq, err := kafka.NewDLQProducer(cfg.Broker.BootstrapServers, cfg.Broker.ClientID, log)
	if err != nil {
		log.Error("failed to create dlq producer", "error", err)
		os.Exit(1)
	}
	defer func() { _ = dlq.Close() }()

	consumer, err := kafka.NewConsumer(handler, []string{"doc.parse.requested", "doc.extract.requested"}, kafka.ConsumerConfig{
		BootstrapServers: cfg.Broker.BootstrapServers, ClientID: cfg.Broker.ClientID, GroupID: "mockworkers",
		SessionTimeoutMs: cfg.Broker.ConsumerSessionTimeoutMs, HeartbeatIntervalMs: cfg.Broker.ConsumerHeartbeatIntervalMs,
		MaxPollIntervalMs: cfg.Broker.ConsumerMaxPollIntervalMs, FetchMaxBytes: cfg.Broker.ConsumerFetchMaxBytes,
		AutoOffsetReset: "earliest", DLQ: dlq,
	}, log)
	if err != nil {
		log.Error("failed to create kafka consumer", "error", err)
		os.Exit(1)
	}

	log.Info("mockworkers service started")
	if err := consumer.Start(ctx); err != nil {
		log.Error("consumer stopped with error", "error", err)
	}
	log.Info("mockworkers service stopped")
}

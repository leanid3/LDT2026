// cmd/engine — consumer результатов воркеров (doc.parse.completed/doc.extract.completed), fan-in/out
// заданий и движок проверок (backend-plan.md §8.3, §8.5). Каталог правил — только демонстрационный
// набор в ../rules (полные 132 параметра — не в этом объёме, docs/architecture.md).
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/config"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine/rules"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/kafka"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/logging"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/minio"
)

const rulesDir = "rules"

func main() {
	log := logging.New("info", "engine")

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	log = logging.New(cfg.Logger.Level, "engine")

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

	ruleSet, err := rules.LoadDir(rulesDir)
	if err != nil {
		log.Error("failed to load rules", "dir", rulesDir, "error", err)
		os.Exit(1)
	}
	log.Info("rules loaded", "count", len(ruleSet))

	handler := engine.NewHandler(database, minioClient, ruleSet)

	dlq, err := kafka.NewDLQProducer(cfg.Broker.BootstrapServers, cfg.Broker.ClientID, log)
	if err != nil {
		log.Error("failed to create dlq producer", "error", err)
		os.Exit(1)
	}
	defer func() { _ = dlq.Close() }()

	consumer, err := kafka.NewConsumer(handler, []string{"doc.parse.completed", "doc.extract.completed"}, kafka.ConsumerConfig{
		BootstrapServers: cfg.Broker.BootstrapServers, ClientID: cfg.Broker.ClientID, GroupID: "engine",
		SessionTimeoutMs: cfg.Broker.ConsumerSessionTimeoutMs, HeartbeatIntervalMs: cfg.Broker.ConsumerHeartbeatIntervalMs,
		MaxPollIntervalMs: cfg.Broker.ConsumerMaxPollIntervalMs, FetchMaxBytes: cfg.Broker.ConsumerFetchMaxBytes,
		AutoOffsetReset: "earliest", DLQ: dlq,
	}, log)
	if err != nil {
		log.Error("failed to create kafka consumer", "error", err)
		os.Exit(1)
	}

	log.Info("engine service started")
	if err := consumer.Start(ctx); err != nil {
		log.Error("consumer stopped with error", "error", err)
	}
	log.Info("engine service stopped")
}

// cmd/rin-sync — отправка результатов в ИАИС «РиН» после PROTOCOL_FINALIZED, ретраи 1/5/15 мин
// (backend-plan.md §8.9).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/config"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/kafka"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/logging"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/rin"
)

func main() {
	log := logging.New("info", "rin-sync")

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	log = logging.New(cfg.Logger.Level, "rin-sync")

	retryDelays, err := parseDurations(cfg.Rin.RetryDelays)
	if err != nil {
		log.Error("invalid RIN_RETRY_DELAYS", "error", err)
		os.Exit(1)
	}

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

	handler := rin.NewHandler(database, rin.NoopSigner{}, rin.Config{
		Endpoint: cfg.Rin.Endpoint, RetryDelays: retryDelays, Timeout: cfg.Rin.Timeout,
	}, log)

	dlq, err := kafka.NewDLQProducer(cfg.Broker.BootstrapServers, cfg.Broker.ClientID, log)
	if err != nil {
		log.Error("failed to create dlq producer", "error", err)
		os.Exit(1)
	}
	defer func() { _ = dlq.Close() }()

	consumer, err := kafka.NewConsumer(handler, []string{"rin.sync.requested"}, kafka.ConsumerConfig{
		BootstrapServers: cfg.Broker.BootstrapServers, ClientID: cfg.Broker.ClientID, GroupID: "rin-sync",
		SessionTimeoutMs: cfg.Broker.ConsumerSessionTimeoutMs, HeartbeatIntervalMs: cfg.Broker.ConsumerHeartbeatIntervalMs,
		MaxPollIntervalMs: cfg.Broker.ConsumerMaxPollIntervalMs, FetchMaxBytes: cfg.Broker.ConsumerFetchMaxBytes,
		AutoOffsetReset: "earliest", DLQ: dlq,
	}, log)
	if err != nil {
		log.Error("failed to create kafka consumer", "error", err)
		os.Exit(1)
	}

	log.Info("rin-sync service started", "endpoint", cfg.Rin.Endpoint, "retry_delays", cfg.Rin.RetryDelays)
	if err := consumer.Start(ctx); err != nil {
		log.Error("consumer stopped with error", "error", err)
	}
	log.Info("rin-sync service stopped")
}

func parseDurations(raw []string) ([]time.Duration, error) {
	out := make([]time.Duration, 0, len(raw))
	for _, s := range raw {
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("parse duration %q: %w", s, err)
		}
		out = append(out, d)
	}
	return out, nil
}

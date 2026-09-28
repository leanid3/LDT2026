// cmd/relay — единственный писатель в Kafka: забирает outbox_events и публикует с ожиданием delivery
// report (backend-plan.md §6.4, docs/architecture.md#transactional-outbox--relay).
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/config"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/kafka"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/logging"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/outbox"
)

func main() {
	log := logging.New("info", "relay")

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	log = logging.New(cfg.Logger.Level, "relay")

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

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		BootstrapServers:  cfg.Broker.BootstrapServers,
		ClientID:          cfg.Broker.ClientID,
		Acks:              cfg.Broker.ProducerAcks,
		EnableIdempotence: cfg.Broker.ProducerEnableIdempotence,
		CompressionType:   cfg.Broker.ProducerCompression,
		Retries:           cfg.Broker.ProducerRetries,
	}, log)
	if err != nil {
		log.Error("failed to create kafka producer", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := producer.Close(); err != nil {
			log.Error("failed to close producer", "error", err)
		}
	}()

	relay := outbox.NewRelay(database.Pool(), producer, log, outbox.RelayConfig{})

	log.Info("relay service started")
	relay.Run(ctx)
	log.Info("relay service stopped")
}

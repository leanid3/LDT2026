package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type Consumer interface {
	Start(ctx context.Context) error
	Stop() error
}

// Handler обрабатывает одно сообщение.
type Handler interface {
	Handle(ctx context.Context, msg *kafka.Message) error
}

// ConsumerConfig — по backend-plan.md §6.3: enable.auto.commit всегда false (коммит только вручную,
// после обработки), isolation.level=read_committed, max.poll.interval.ms достаточно большой для
// долгих заданий разбора/извлечения, fetch.max.bytes ограничивает размер одного fetch.
type ConsumerConfig struct {
	BootstrapServers    string
	ClientID            string
	GroupID             string
	SessionTimeoutMs    int
	HeartbeatIntervalMs int
	MaxPollIntervalMs   int // default >= 1_800_000 (30 мин, backend-plan.md §6.3)
	FetchMaxBytes       int
	AutoOffsetReset     string // earliest, latest, none (по умолчанию latest)
}

func (c ConsumerConfig) withDefaults() ConsumerConfig {
	if c.MaxPollIntervalMs <= 0 {
		c.MaxPollIntervalMs = 1_800_000
	}
	if c.FetchMaxBytes <= 0 {
		c.FetchMaxBytes = 50 * 1024 * 1024
	}
	return c
}

type consumer struct {
	client      *kafka.Consumer
	topics      []string
	handler     Handler
	commitChain chan *kafka.Message
	wg          sync.WaitGroup
	once        sync.Once
	log         *slog.Logger

	brokerDownCount   int
	brokerDownStart   time.Time
	lastBrokerDownLog time.Time
}

// NewConsumer создаёт consumer с ручным коммитом офсетов.
//
// TODO(M1): цикл коммита сейчас батчится по таймеру/размеру — при переходе на platform/outbox и
// idempotency (backend-plan.md §6.3) заменить на: BEGIN -> INSERT consumed_events ON CONFLICT DO NOTHING
// -> бизнес-логика -> INSERT outbox_events -> COMMIT -> commit offset (по одному сообщению за раз).
func NewConsumer(handler Handler, topics []string, cfg ConsumerConfig, log *slog.Logger) (Consumer, error) {
	cfg = cfg.withDefaults()

	kcfg := kafka.ConfigMap{
		"bootstrap.servers":     cfg.BootstrapServers,
		"client.id":             cfg.ClientID,
		"group.id":              cfg.GroupID,
		"enable.auto.commit":    false,
		"isolation.level":       "read_committed",
		"session.timeout.ms":    cfg.SessionTimeoutMs,
		"heartbeat.interval.ms": cfg.HeartbeatIntervalMs,
		"max.poll.interval.ms":  cfg.MaxPollIntervalMs,
		"fetch.max.bytes":       cfg.FetchMaxBytes,
	}

	if cfg.AutoOffsetReset != "" {
		kcfg["auto.offset.reset"] = cfg.AutoOffsetReset
	}

	c, err := kafka.NewConsumer(&kcfg)
	if err != nil {
		return nil, fmt.Errorf("create kafka consumer: %w", err)
	}

	return &consumer{
		client:      c,
		topics:      topics,
		handler:     handler,
		commitChain: make(chan *kafka.Message, 1000),
		log:         log,
	}, nil
}

func (c *consumer) Start(ctx context.Context) error {
	if err := c.client.SubscribeTopics(c.topics, nil); err != nil {
		return fmt.Errorf("subscribe to topics %s: %w", strings.Join(c.topics, ","), err)
	}

	c.wg.Add(2)
	go c.pollLoop(ctx)
	go c.commitLoop(ctx)

	<-ctx.Done()
	return c.Stop()
}

func (c *consumer) Stop() error {
	c.once.Do(func() {
		close(c.commitChain)
		if err := c.client.Close(); err != nil {
			c.log.Error("failed to close kafka consumer", "error", err)
		}
		c.wg.Wait()
	})
	return nil
}

func (c *consumer) pollLoop(ctx context.Context) {
	defer c.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := c.client.ReadMessage(100 * time.Millisecond)
		if err != nil {
			c.handleReadError(ctx, err)
			continue
		}

		c.markBrokerUp()

		if err := c.handler.Handle(ctx, msg); err != nil {
			c.log.Error("failed to handle message", "error", err, "topic", *msg.TopicPartition.Topic)
			continue
		}
		c.commitChain <- msg
	}
}

func (c *consumer) handleReadError(ctx context.Context, err error) {
	kafkaErr, ok := err.(kafka.Error)
	if !ok {
		c.log.Error("failed to read message", "error", err)
		sleep(ctx, time.Second)
		return
	}

	switch kafkaErr.Code() {
	case kafka.ErrTimedOut:
		c.markBrokerUp()
	case kafka.ErrAllBrokersDown, kafka.ErrBrokerNotAvailable:
		if c.brokerDownCount == 0 {
			c.brokerDownStart = time.Now()
			c.lastBrokerDownLog = time.Now()
		}
		c.brokerDownCount++

		if c.brokerDownCount == 1 || time.Since(c.lastBrokerDownLog) >= 60*time.Second {
			c.log.Warn("kafka brokers unavailable", "error", err, "retry_attempt", c.brokerDownCount,
				"downtime", time.Since(c.brokerDownStart))
			c.lastBrokerDownLog = time.Now()
		}

		backoff := min(time.Duration(c.brokerDownCount)*2*time.Second, 30*time.Second)
		sleep(ctx, backoff)
	default:
		c.log.Error("kafka read error", "code", kafkaErr.Code(), "error", err)
		sleep(ctx, time.Second)
	}
}

func (c *consumer) markBrokerUp() {
	if c.brokerDownCount == 0 {
		return
	}
	c.log.Info("kafka brokers recovered", "downtime", time.Since(c.brokerDownStart), "retry_attempts", c.brokerDownCount)
	c.brokerDownCount = 0
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// commitLoop — батч-коммит каждые 10 сообщений или 5 сек.
func (c *consumer) commitLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	batch := make([]*kafka.Message, 0, 10)
	for {
		select {
		case <-ctx.Done():
			c.drainCommit(batch)
			return
		case msg, ok := <-c.commitChain:
			if !ok {
				c.drainCommit(batch)
				return
			}
			batch = append(batch, msg)
			if len(batch) >= 10 {
				c.drainCommit(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				c.drainCommit(batch)
				batch = batch[:0]
			}
		}
	}
}

func (c *consumer) drainCommit(batch []*kafka.Message) {
	for _, msg := range batch {
		if _, err := c.client.CommitMessage(msg); err != nil {
			c.log.Error("failed to commit message", "error", err)
		}
	}
}

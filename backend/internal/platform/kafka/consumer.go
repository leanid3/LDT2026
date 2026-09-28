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

	// MaxAttempts попыток обработки сообщения (по умолчанию 3), между ними — линейный backoff
	// RetryBackoff (по умолчанию 1с). backend-plan.md §6.3.
	MaxAttempts  int
	RetryBackoff time.Duration
	// DLQ — куда отправить сообщение, которое не удалось обработать за MaxAttempts попыток
	// (<topic>.dlq, исходное тело + причина в заголовках). nil — сообщение только логируется и
	// пропускается: без DLQ это тихая потеря события, поэтому все сервисы должны задавать DLQ.
	DLQ DLQSender
}

// DLQSender — то, чем consumer пишет в dead-letter топик. kafka.Producer подходит как есть.
type DLQSender interface {
	SendRaw(ctx context.Context, topic string, key string, headers map[string]string, value []byte) error
}

func (c ConsumerConfig) withDefaults() ConsumerConfig {
	if c.MaxPollIntervalMs <= 0 {
		c.MaxPollIntervalMs = 1_800_000
	}
	if c.FetchMaxBytes <= 0 {
		c.FetchMaxBytes = 50 * 1024 * 1024
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.RetryBackoff <= 0 {
		c.RetryBackoff = time.Second
	}
	return c
}

type consumer struct {
	client      *kafka.Consumer
	topics      []string
	handler     Handler
	commitChain chan *kafka.Message
	cfg         ConsumerConfig
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
		cfg:         cfg,
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

		if !c.processWithRetry(ctx, msg) {
			return // ctx отменён посреди обработки — offset не коммитим, сообщение придёт снова
		}
		c.commitChain <- msg
	}
}

// processWithRetry обрабатывает сообщение до MaxAttempts раз; не справилось — уводит в <topic>.dlq.
// Возвращает true, если сообщение "закрыто" (обработано или сохранено в DLQ) и его offset можно
// коммитить. false — только если контекст отменён: тогда сообщение не трогаем.
//
// Без этого ошибка обработчика означала пропуск: следующий успешный commit "перепрыгивал" сбойное
// сообщение, и событие терялось молча (процесс навсегда зависал в PARSING).
func (c *consumer) processWithRetry(ctx context.Context, msg *kafka.Message) bool {
	topic := ""
	if msg.TopicPartition.Topic != nil {
		topic = *msg.TopicPartition.Topic
	}

	var lastErr error
	for attempt := 1; attempt <= c.cfg.MaxAttempts; attempt++ {
		if lastErr = c.handler.Handle(ctx, msg); lastErr == nil {
			return true
		}
		c.log.Warn("failed to handle message", "error", lastErr, "topic", topic, "offset", msg.TopicPartition.Offset,
			"attempt", attempt, "max_attempts", c.cfg.MaxAttempts)
		if ctx.Err() != nil {
			return false
		}
		if attempt < c.cfg.MaxAttempts {
			sleep(ctx, time.Duration(attempt)*c.cfg.RetryBackoff)
		}
	}

	if c.cfg.DLQ == nil {
		c.log.Error("message dropped after retries: no DLQ configured", "topic", topic, "error", lastErr)
		return true
	}
	return c.sendToDLQ(ctx, msg, topic, lastErr)
}

// sendToDLQ повторяет отправку, пока не получится: если положить в DLQ не вышло, коммитить нельзя —
// иначе сообщение потеряется. Блокирует партицию, но это лучше тихой потери.
func (c *consumer) sendToDLQ(ctx context.Context, msg *kafka.Message, topic string, cause error) bool {
	headers := map[string]string{
		"dlq_error":            cause.Error(),
		"dlq_source_topic":     topic,
		"dlq_source_partition": fmt.Sprint(msg.TopicPartition.Partition),
		"dlq_source_offset":    msg.TopicPartition.Offset.String(),
		"dlq_failed_at":        time.Now().UTC().Format(time.RFC3339),
	}
	for _, h := range msg.Headers {
		if _, taken := headers[h.Key]; !taken {
			headers[h.Key] = string(h.Value)
		}
	}
	for {
		err := c.cfg.DLQ.SendRaw(ctx, topic+".dlq", string(msg.Key), headers, msg.Value)
		if err == nil {
			c.log.Error("message moved to DLQ", "topic", topic, "dlq", topic+".dlq", "offset", msg.TopicPartition.Offset, "error", cause)
			return true
		}
		c.log.Error("failed to publish to DLQ, will retry", "error", err, "topic", topic)
		if ctx.Err() != nil {
			return false
		}
		sleep(ctx, 2*time.Second)
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

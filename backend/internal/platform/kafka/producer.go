package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Producer — интерфейс для DI и тестов.
//
// По backend-plan.md §6.4 в Kafka напрямую пишет только cmd/relay (из outbox);
// остальные сервисы публикуют события только через platform/outbox в той же транзакции с бизнес-изменением.
type Producer interface {
	Send(ctx context.Context, topic string, key string, headers map[string]string, value interface{}) error
	Close() error
}

type ProducerConfig struct {
	BootstrapServers  string
	ClientID          string
	Acks              string
	EnableIdempotence bool
	CompressionType   string
	Retries           int
}

type producer struct {
	client *kafka.Producer
	log    *slog.Logger
	once   sync.Once
}

func NewProducer(cfg ProducerConfig, log *slog.Logger) (Producer, error) {
	kcfg := kafka.ConfigMap{
		"bootstrap.servers": cfg.BootstrapServers,
		"client.id":         cfg.ClientID,
	}

	if cfg.Acks != "" {
		kcfg["acks"] = cfg.Acks
	} else {
		kcfg["acks"] = "-1"
	}

	if cfg.CompressionType != "" {
		kcfg["compression.type"] = cfg.CompressionType
	}

	if cfg.Retries > 0 {
		kcfg["retries"] = cfg.Retries
	}

	kcfg["enable.idempotence"] = cfg.EnableIdempotence

	p, err := kafka.NewProducer(&kcfg)
	if err != nil {
		return nil, fmt.Errorf("create kafka producer: %w", err)
	}

	return &producer{client: p, log: log}, nil
}

// Send публикует сообщение и ждёт delivery report (не только Produce — проверяет ошибку доставки).
func (p *producer) Send(ctx context.Context, topic, key string, headers map[string]string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            []byte(key),
		Headers:        toKafkaHeaders(headers),
		Value:          data,
	}

	deliveryChan := make(chan kafka.Event, 1)
	if err := p.client.Produce(msg, deliveryChan); err != nil {
		return fmt.Errorf("produce message: %w", err)
	}

	select {
	case <-ctx.Done():
		go func() {
			select {
			case ev := <-deliveryChan:
				if m, ok := ev.(*kafka.Message); ok && m.TopicPartition.Error != nil {
					p.log.Warn("message delivery failed after ctx cancel", "key", key, "error", m.TopicPartition.Error)
				}
			case <-time.After(100 * time.Millisecond):
			}
		}()
		return ctx.Err()
	case ev := <-deliveryChan:
		m, ok := ev.(*kafka.Message)
		if !ok {
			return nil
		}
		if m.TopicPartition.Error != nil {
			return fmt.Errorf("delivery failed: %w", m.TopicPartition.Error)
		}
		return nil
	}
}

func toKafkaHeaders(headers map[string]string) []kafka.Header {
	out := make([]kafka.Header, 0, len(headers))
	for k, v := range headers {
		out = append(out, kafka.Header{Key: k, Value: []byte(v)})
	}
	return out
}

// Close делает Flush и закрывает продюсер, дренируя оставшиеся delivery-события.
func (p *producer) Close() error {
	p.once.Do(func() {
		p.client.Flush(15 * 1000)

		for i := 0; i < 1000; i++ {
			select {
			case ev := <-p.client.Events():
				if m, ok := ev.(*kafka.Message); ok && m.TopicPartition.Error != nil {
					p.log.Warn("undelivered message on close", "error", m.TopicPartition.Error)
				}
			default:
				p.client.Close()
				return
			}
		}
		p.client.Close()
	})
	return nil
}

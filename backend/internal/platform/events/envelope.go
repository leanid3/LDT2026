// Package events — сторона consumer'а общего конверта событий (backend-plan.md §6.1). Зеркало
// outbox.Envelope (producer-сторона, internal/platform/outbox), но с Data как json.RawMessage —
// конкретный consumer сам знает, во что декодировать payload по EventType.
package events

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type Envelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	SchemaVersion int             `json:"schema_version"`
	OccurredAt    time.Time       `json:"occurred_at"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	ObjectID      string          `json:"object_id,omitempty"`
	ProcessID     string          `json:"process_id,omitempty"`
	Data          json.RawMessage `json:"data"`
}

// Decode разбирает сырое сообщение Kafka в конверт.
func Decode(msg *kafka.Message) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(msg.Value, &e); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	return e, nil
}

// DecodeData разбирает поле data конверта в конкретный тип payload.
func (e Envelope) DecodeData(v any) error {
	if err := json.Unmarshal(e.Data, v); err != nil {
		return fmt.Errorf("decode envelope data (event_type=%s): %w", e.EventType, err)
	}
	return nil
}

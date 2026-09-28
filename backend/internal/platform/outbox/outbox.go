// Package outbox реализует transactional outbox (docs/architecture.md#transactional-outbox--relay):
// бизнес-код никогда не пишет в Kafka напрямую (backend/CLAUDE.md, правило 5) — вместо этого в той же
// транзакции, что бизнес-изменение, пишет строку в outbox_events. cmd/relay — единственный писатель
// в Kafka, читает эту таблицу и публикует.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Envelope — общий конверт события (backend-plan.md §6.1).
type Envelope struct {
	EventID       uuid.UUID `json:"event_id"`
	EventType     string    `json:"event_type"`
	SchemaVersion int       `json:"schema_version"`
	OccurredAt    time.Time `json:"occurred_at"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	ObjectID      string    `json:"object_id,omitempty"`
	ProcessID     string    `json:"process_id,omitempty"`
	Data          any       `json:"data"`
}

// Event — то, что бизнес-код передаёт в Insert.
type Event struct {
	AggregateID   uuid.UUID
	EventType     string
	Topic         string
	MsgKey        string // ключ сообщения Kafka — object_id (backend-plan.md §6.1)
	CorrelationID string
	ObjectID      string
	ProcessID     string
	Data          any
}

// Insert пишет событие в outbox_events внутри переданной транзакции (unit of work, db.WithTx).
func Insert(ctx context.Context, tx pgx.Tx, ev Event) error {
	envelope := Envelope{
		EventID:       uuid.New(),
		EventType:     ev.EventType,
		SchemaVersion: 1,
		OccurredAt:    time.Now().UTC(),
		CorrelationID: ev.CorrelationID,
		ObjectID:      ev.ObjectID,
		ProcessID:     ev.ProcessID,
		Data:          ev.Data,
	}

	payload, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal outbox envelope: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (event_id, aggregate_id, event_type, topic, msg_key, payload)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, envelope.EventID, ev.AggregateID, ev.EventType, ev.Topic, ev.MsgKey, payload)
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

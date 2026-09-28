package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// record — строка outbox_events, забранная relay'ем на публикацию.
type record struct {
	EventID  uuid.UUID
	Topic    string
	MsgKey   string
	Payload  []byte
	Attempts int
}

// headerFields — минимум полей конверта, нужных для заголовков Kafka (backend-plan.md §6.1).
type headerFields struct {
	EventType     string `json:"event_type"`
	CorrelationID string `json:"correlation_id"`
}

type Sender interface {
	SendRaw(ctx context.Context, topic, key string, headers map[string]string, data []byte) error
}

type RelayConfig struct {
	BatchSize      int
	PollInterval   time.Duration
	MaxBackoff     time.Duration
	StuckAfter     time.Duration // publishing дольше этого — считается зависшим, переподхватывается
	PublishTimeout time.Duration
}

func (c RelayConfig) withDefaults() RelayConfig {
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.PollInterval <= 0 {
		c.PollInterval = time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 60 * time.Second
	}
	if c.StuckAfter <= 0 {
		c.StuckAfter = 2 * time.Minute
	}
	if c.PublishTimeout <= 0 {
		c.PublishTimeout = 10 * time.Second
	}
	return c
}

// Relay — реализация cmd/relay (backend-plan.md §6.4, docs/architecture.md#transactional-outbox--relay):
// claim FOR UPDATE SKIP LOCKED -> публикация -> published/failed с backoff. Несколько реплик безопасны
// за счёт SKIP LOCKED; зависшие publishing старше StuckAfter переподхватываются.
type Relay struct {
	pool     *pgxpool.Pool
	producer Sender
	log      *slog.Logger
	cfg      RelayConfig
}

func NewRelay(pool *pgxpool.Pool, producer Sender, log *slog.Logger, cfg RelayConfig) *Relay {
	return &Relay{pool: pool, producer: producer, log: log, cfg: cfg.withDefaults()}
}

// Run опрашивает outbox_events до отмены ctx.
func (r *Relay) Run(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Tick(ctx); err != nil {
				r.log.Error("relay tick failed", "error", err)
			}
		}
	}
}

// Tick — один проход: переподхват зависших, claim пачки, публикация. Возвращает число обработанных
// записей (полезно в тестах).
func (r *Relay) Tick(ctx context.Context) error {
	if err := r.requeueStuck(ctx); err != nil {
		return fmt.Errorf("requeue stuck: %w", err)
	}

	records, err := r.claimBatch(ctx)
	if err != nil {
		return fmt.Errorf("claim batch: %w", err)
	}

	for _, rec := range records {
		r.publishOne(ctx, rec)
	}
	return nil
}

func (r *Relay) requeueStuck(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE outbox_events
		SET status = 'pending', locked_at = NULL
		WHERE status = 'publishing' AND locked_at < now() - $1::interval
	`, r.cfg.StuckAfter.String())
	return err
}

func (r *Relay) claimBatch(ctx context.Context) ([]record, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT event_id, topic, msg_key, payload, attempts
		FROM outbox_events
		WHERE status IN ('pending', 'failed') AND available_at <= now()
		ORDER BY created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, r.cfg.BatchSize)
	if err != nil {
		return nil, err
	}

	var records []record
	var ids []uuid.UUID
	for rows.Next() {
		var rec record
		if err := rows.Scan(&rec.EventID, &rec.Topic, &rec.MsgKey, &rec.Payload, &rec.Attempts); err != nil {
			rows.Close()
			return nil, err
		}
		records = append(records, rec)
		ids = append(ids, rec.EventID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	if len(ids) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE outbox_events SET status = 'publishing', locked_at = now() WHERE event_id = ANY($1)
		`, ids); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return records, nil
}

func (r *Relay) publishOne(ctx context.Context, rec record) {
	var hf headerFields
	_ = json.Unmarshal(rec.Payload, &hf)

	headers := map[string]string{"event_type": hf.EventType}
	if hf.CorrelationID != "" {
		headers["correlation_id"] = hf.CorrelationID
	}

	pubCtx, cancel := context.WithTimeout(ctx, r.cfg.PublishTimeout)
	err := r.producer.SendRaw(pubCtx, rec.Topic, rec.MsgKey, headers, rec.Payload)
	cancel()

	if err != nil {
		r.markFailed(ctx, rec, err)
		return
	}
	r.markPublished(ctx, rec)
}

func (r *Relay) markPublished(ctx context.Context, rec record) {
	if _, err := r.pool.Exec(ctx, `
		UPDATE outbox_events SET status = 'published', published_at = now() WHERE event_id = $1
	`, rec.EventID); err != nil {
		r.log.Error("failed to mark outbox event published", "event_id", rec.EventID, "error", err)
	}
}

func (r *Relay) markFailed(ctx context.Context, rec record, sendErr error) {
	attempts := rec.Attempts + 1
	backoff := time.Duration(1<<min(attempts, 10)) * time.Second
	if backoff > r.cfg.MaxBackoff {
		backoff = r.cfg.MaxBackoff
	}

	r.log.Warn("outbox publish failed, will retry", "event_id", rec.EventID, "topic", rec.Topic,
		"attempts", attempts, "backoff", backoff, "error", sendErr)

	if _, err := r.pool.Exec(ctx, `
		UPDATE outbox_events
		SET status = 'failed', attempts = $2, available_at = now() + $3::interval, last_error = $4
		WHERE event_id = $1
	`, rec.EventID, attempts, backoff.String(), sendErr.Error()); err != nil {
		r.log.Error("failed to mark outbox event failed", "event_id", rec.EventID, "error", err)
	}
}

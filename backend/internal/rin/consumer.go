package rin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/events"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/idempotency"
)

const consumerName = "rin-sync"

type SyncRequestedPayload struct {
	ProcessID string `json:"process_id"`
}

type Config struct {
	Endpoint    string
	RetryDelays []time.Duration // backend-plan.md §8.9: 1/5/15 мин по умолчанию, конфигурируемо для демо
	Timeout     time.Duration
}

type Handler struct {
	database *db.DB
	httpc    *http.Client
	signer   Signer
	cfg      Config
	log      *slog.Logger
}

func NewHandler(database *db.DB, signer Signer, cfg Config, log *slog.Logger) *Handler {
	return &Handler{database: database, httpc: &http.Client{Timeout: cfg.Timeout}, signer: signer, cfg: cfg, log: log}
}

func (h *Handler) Handle(ctx context.Context, msg *kafka.Message) error {
	env, err := events.Decode(msg)
	if err != nil {
		return err
	}
	if env.EventType != "rin.sync.requested" {
		return nil
	}

	_, err = idempotency.Once(ctx, h.database, consumerName, env.EventID, func(tx pgx.Tx) error {
		return h.sync(ctx, tx, env)
	})
	return err
}

// sync — backend-plan.md §8.9: ретраи на 5xx/таймаут (1/5/15 мин по умолчанию), затем SYNC_FAILED.
// Допустимо блокировать consumer на время ретраев — max.poll.interval.ms выставлен под это
// (backend-plan.md §6.3, docs/architecture.md).
func (h *Handler) sync(ctx context.Context, tx pgx.Tx, env events.Envelope) error {
	var payload SyncRequestedPayload
	if err := env.DecodeData(&payload); err != nil {
		return err
	}
	processID, err := uuid.Parse(payload.ProcessID)
	if err != nil {
		return fmt.Errorf("parse process_id: %w", err)
	}

	reqPayload, err := BuildPayload(ctx, h.database, processID, h.signer)
	if err != nil {
		return fmt.Errorf("build rin payload: %w", err)
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return fmt.Errorf("marshal rin payload: %w", err)
	}

	attempts := 1 + len(h.cfg.RetryDelays)
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			delay := h.cfg.RetryDelays[attempt-1]
			h.log.Warn("rin sync failed, retrying", "process_id", processID, "attempt", attempt, "delay", delay, "error", lastErr)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := h.post(ctx, body)
		if err == nil {
			h.log.Info("rin sync succeeded", "process_id", processID, "attempt", attempt+1)
			return updateSyncStatus(ctx, tx, processID, "SYNCED")
		}

		lastErr = err
		if !errors.Is(err, errRetryable) {
			h.log.Error("rin sync failed with non-retryable error", "process_id", processID, "error", err)
			return updateSyncStatus(ctx, tx, processID, "SYNC_FAILED")
		}
	}

	h.log.Error("rin sync exhausted retries", "process_id", processID, "error", lastErr)
	return updateSyncStatus(ctx, tx, processID, "SYNC_FAILED")
}

// errRetryable — маркер: ретраить (5xx/таймаут), в отличие от 4xx (backend-plan.md §8.9: "Ретраи на
// 5xx/таймаут"; 4xx — структурная ошибка запроса, повтор её не исправит).
var errRetryable = errors.New("rin: retryable error")

func (h *Handler) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("%w: http request: %v", errRetryable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout {
		return fmt.Errorf("%w: rin returned %d", errRetryable, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("rin returned %d (client error, not retried)", resp.StatusCode)
	}
	return nil
}

func updateSyncStatus(ctx context.Context, tx pgx.Tx, processID uuid.UUID, status string) error {
	_, err := tx.Exec(ctx, `
		UPDATE protocols SET sync_status = $2
		WHERE process_id = $1 AND version = (SELECT max(version) FROM protocols WHERE process_id = $1)
	`, processID, status)
	if err != nil {
		return fmt.Errorf("update protocol sync_status: %w", err)
	}
	return nil
}

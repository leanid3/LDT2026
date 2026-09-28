// Package mockworkers — НАША заглушка вместо реальных Python parse-worker/LLM-worker
// (docs/architecture.md, docs/backend-plan.md §12: воркеры — не наша зона ответственности сейчас,
// но конвейер нужно на чём-то проверять). Не продакшен-код: намеренно упрощённая, детерминированная
// генерация "фактов", чтобы cmd/engine было что обрабатывать при сквозном прогоне без Python-команды.
package mockworkers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	miniogo "github.com/minio/minio-go/v7"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/events"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/idempotency"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/minio"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/outbox"
)

const consumerName = "mockworkers"

// ParseRequestedPayload — data doc.parse.requested (contracts/events/doc.parse.requested.schema.json).
type ParseRequestedPayload struct {
	JobID       string  `json:"job_id"`
	FileID      string  `json:"file_id"`
	DocStage    string  `json:"doc_stage"`
	Discipline  *string `json:"discipline"`
	SHA256      *string `json:"sha256"`
	StorageKey  string  `json:"storage_key"`
	ContentType *string `json:"content_type"`
	PageFrom    *int    `json:"page_from"`
	PageTo      *int    `json:"page_to"`
}

// ExtractRequestedPayload — data doc.extract.requested (contracts/events/doc.extract.requested.schema.json).
type ExtractRequestedPayload struct {
	JobID  string                  `json:"job_id"`
	Files  []engine.ExtractFileRef `json:"files"`
	Params []string                `json:"params"`
}

// Handler — единый consumer на doc.parse.requested/doc.extract.requested.
type Handler struct {
	database *db.DB
	minio    *minio.Client
	log      *slog.Logger
}

func NewHandler(database *db.DB, minioClient *minio.Client, log *slog.Logger) *Handler {
	return &Handler{database: database, minio: minioClient, log: log}
}

func (h *Handler) Handle(ctx context.Context, msg *kafka.Message) error {
	env, err := events.Decode(msg)
	if err != nil {
		return err
	}

	switch env.EventType {
	case "doc.parse.requested":
		return h.handleParseRequested(ctx, env)
	case "doc.extract.requested":
		return h.handleExtractRequested(ctx, env)
	default:
		return nil
	}
}

// handleParseRequested — "парсит" файл за один шаг: сразу публикует doc.parse.completed с quality=OK
// и синтетическим layout_key. Реальный parse-worker делает это по-настоящему (OCR/векторный текст);
// мы просто эмулируем контракт.
func (h *Handler) handleParseRequested(ctx context.Context, env events.Envelope) error {
	_, err := idempotency.Once(ctx, h.database, consumerName, env.EventID, func(tx pgx.Tx) error {
		var payload ParseRequestedPayload
		if err := env.DecodeData(&payload); err != nil {
			return err
		}

		h.log.Info("mock-parsing file", "job_id", payload.JobID, "file_id", payload.FileID)

		return outbox.Insert(ctx, tx, outbox.Event{
			AggregateID: uuid.MustParse(env.ProcessID), EventType: "doc.parse.completed", Topic: "doc.parse.completed",
			MsgKey: env.ObjectID, ObjectID: env.ObjectID, ProcessID: env.ProcessID,
			Data: map[string]any{
				"job_id": payload.JobID, "file_id": payload.FileID, "quality": "OK",
				"layout_key": fmt.Sprintf("mock-layout/%s.json", payload.JobID),
			},
		})
	})
	return err
}

// handleExtractRequested — вместо реального LLM-извлечения пишет 2 синтетических факта под
// правила backend/rules/M-001.yaml (numeric_equal) и M-041.yaml (threshold_min 0.9 м) в MinIO как
// JSONL, затем публикует doc.extract.completed. M-001 намеренно расходится (100 в ПД vs 90 в РД) —
// чтобы при сквозной проверке было что решать инспектору; M-041 (ширина двери 1.2 м) намеренно в норме.
func (h *Handler) handleExtractRequested(ctx context.Context, env events.Envelope) error {
	_, err := idempotency.Once(ctx, h.database, consumerName, env.EventID, func(tx pgx.Tx) error {
		var payload ExtractRequestedPayload
		if err := env.DecodeData(&payload); err != nil {
			return err
		}
		if len(payload.Files) == 0 {
			h.log.Warn("extract requested with no files, skipping fact generation", "job_id", payload.JobID)
			return outbox.Insert(ctx, tx, outbox.Event{
				AggregateID: uuid.MustParse(env.ProcessID), EventType: "doc.extract.completed", Topic: "doc.extract.completed",
				MsgKey: env.ObjectID, ObjectID: env.ObjectID, ProcessID: env.ProcessID,
				Data: map[string]any{"job_id": payload.JobID, "facts_key": ""},
			})
		}

		fileID := payload.Files[0].FileID
		facts := []engine.Fact{
			{FactID: uuid.NewString(), FileID: fileID, Stage: "PD", ParamCode: "M-001", ValueRaw: "100",
				Unit: "м²", Page: 1, Quote: "Площадь застройки: 100 м²", Confidence: 0.95, Method: "mock",
				Quality: "OK", ExtractorVersion: "mockworkers@0.1"},
			{FactID: uuid.NewString(), FileID: fileID, Stage: "RD", ParamCode: "M-001", ValueRaw: "90",
				Unit: "м²", Page: 3, Quote: "Площадь застройки: 90 м²", Confidence: 0.92, Method: "mock",
				Quality: "OK", ExtractorVersion: "mockworkers@0.1"},
			{FactID: uuid.NewString(), FileID: fileID, Stage: "RD", ParamCode: "M-041", ValueRaw: "1.2",
				Unit: "м", Page: 5, Quote: "Ширина проезда: 1.2 м", Confidence: 0.9, Method: "mock",
				Quality: "OK", ExtractorVersion: "mockworkers@0.1"},
		}

		factsKey := fmt.Sprintf("facts/%s.jsonl", payload.JobID)
		if err := h.uploadFactsJSONL(ctx, factsKey, facts); err != nil {
			return fmt.Errorf("upload facts jsonl: %w", err)
		}

		h.log.Info("mock-extracted facts", "job_id", payload.JobID, "facts_key", factsKey, "count", len(facts))

		return outbox.Insert(ctx, tx, outbox.Event{
			AggregateID: uuid.MustParse(env.ProcessID), EventType: "doc.extract.completed", Topic: "doc.extract.completed",
			MsgKey: env.ObjectID, ObjectID: env.ObjectID, ProcessID: env.ProcessID,
			Data: map[string]any{"job_id": payload.JobID, "facts_key": factsKey, "model_version": "mockworkers@0.1"},
		})
	})
	return err
}

func (h *Handler) uploadFactsJSONL(ctx context.Context, key string, facts []engine.Fact) error {
	var buf bytes.Buffer
	for _, f := range facts {
		line, err := json.Marshal(f)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	uploadCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := h.minio.Raw().PutObject(uploadCtx, h.minio.BucketName(), key, &buf, int64(buf.Len()), miniogo.PutObjectOptions{
		ContentType: "application/x-ndjson",
	})
	return err
}

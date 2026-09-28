package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	miniogo "github.com/minio/minio-go/v7"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine/rules"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/events"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/idempotency"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/minio"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/outbox"
)

const consumerName = "engine"

// ParseCompletedPayload — data события doc.parse.completed (backend-plan.md §6.2).
type ParseCompletedPayload struct {
	JobID     string          `json:"job_id"`
	FileID    string          `json:"file_id"`
	Quality   string          `json:"quality"`
	LayoutKey string          `json:"layout_key"`
	PageFrom  *int            `json:"page_from"`
	PageTo    *int            `json:"page_to"`
	Stats     json.RawMessage `json:"stats"`
	Error     string          `json:"error"`
}

// ExtractCompletedPayload — data события doc.extract.completed.
type ExtractCompletedPayload struct {
	JobID         string `json:"job_id"`
	FactsKey      string `json:"facts_key"`
	SuspicionsKey string `json:"suspicions_key"`
	ModelVersion  string `json:"model_version"`
	// Error — воркер не смог обработать задание (после своих ретраев). Задание помечается FAILED,
	// процесс не блокируется (backend-plan.md §8.3, ТЗ 9.1): факты от него просто не придут.
	Error string `json:"error"`
}

// Handler — единый consumer на оба топика doc.parse.completed/doc.extract.completed (fan-in/fan-out,
// backend-plan.md §8.3, §8.5).
type Handler struct {
	database *db.DB
	minio    *minio.Client
	ruleSet  map[string]rules.Rule
}

func NewHandler(database *db.DB, minioClient *minio.Client, ruleSet map[string]rules.Rule) *Handler {
	return &Handler{database: database, minio: minioClient, ruleSet: ruleSet}
}

func (h *Handler) Handle(ctx context.Context, msg *kafka.Message) error {
	env, err := events.Decode(msg)
	if err != nil {
		return err
	}

	switch env.EventType {
	case "doc.parse.completed":
		return h.handleParseCompleted(ctx, env)
	case "doc.extract.completed":
		return h.handleExtractCompleted(ctx, env)
	default:
		return nil
	}
}

func (h *Handler) handleParseCompleted(ctx context.Context, env events.Envelope) error {
	_, err := idempotency.Once(ctx, h.database, consumerName, env.EventID, func(tx pgx.Tx) error {
		var payload ParseCompletedPayload
		if err := env.DecodeData(&payload); err != nil {
			return err
		}
		jobID, err := uuid.Parse(payload.JobID)
		if err != nil {
			return fmt.Errorf("parse job_id: %w", err)
		}
		processID, err := uuid.Parse(env.ProcessID)
		if err != nil {
			return fmt.Errorf("parse process_id: %w", err)
		}

		status := "DONE"
		if payload.Quality == "FAILED" || payload.Quality == "ABSTAIN" {
			status = "FAILED"
		}
		if _, err := tx.Exec(ctx, `
			UPDATE parse_jobs SET status = $2, quality = $3, layout_key = $4, finished_at = now() WHERE id = $1
		`, jobID, status, nullIfEmpty(payload.Quality), nullIfEmpty(payload.LayoutKey)); err != nil {
			return fmt.Errorf("update parse_job: %w", err)
		}

		return h.fanInToExtract(ctx, tx, env, processID)
	})
	return err
}

// fanInToExtract — когда все parse_jobs процесса в конечном статусе, создаёт один extract_job на
// процесс (упрощение относительно §8.3, которое допускает частичный extract по затронутым
// параметрам — инкрементальность не в этом объёме, см. docs/backend-plan.md §12) и публикует
// doc.extract.requested.
func (h *Handler) fanInToExtract(ctx context.Context, tx pgx.Tx, env events.Envelope, processID uuid.UUID) error {
	var pending int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM parse_jobs WHERE process_id = $1 AND status NOT IN ('DONE', 'FAILED')
	`, processID).Scan(&pending); err != nil {
		return fmt.Errorf("count pending parse_jobs: %w", err)
	}
	if pending > 0 {
		return nil
	}

	var alreadyExtracted int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM extract_jobs WHERE process_id = $1`, processID).Scan(&alreadyExtracted); err != nil {
		return fmt.Errorf("count extract_jobs: %w", err)
	}
	if alreadyExtracted > 0 {
		return nil
	}

	extractJobID := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO extract_jobs (id, process_id, status) VALUES ($1, $2, 'PENDING')`, extractJobID, processID); err != nil {
		return fmt.Errorf("insert extract_job: %w", err)
	}

	fileRefs, err := currentAcceptedFileRefs(ctx, tx, processID)
	if err != nil {
		return err
	}

	if err := outbox.Insert(ctx, tx, outbox.Event{
		AggregateID: processID, EventType: "doc.extract.requested", Topic: "doc.extract.requested",
		MsgKey: env.ObjectID, ObjectID: env.ObjectID, ProcessID: env.ProcessID,
		Data: map[string]any{"job_id": extractJobID, "files": fileRefs},
	}); err != nil {
		return fmt.Errorf("outbox doc.extract.requested: %w", err)
	}
	return nil
}

// ExtractFileRef — элемент files[] в data события doc.extract.requested (backend-plan.md §6.2).
type ExtractFileRef struct {
	FileID     string   `json:"file_id"`
	Stage      string   `json:"stage"`
	Discipline string   `json:"discipline,omitempty"`
	LayoutKeys []string `json:"layout_keys"` // layout_key каждого DONE-задания парсинга, по порядку страниц
}

func currentAcceptedFileRefs(ctx context.Context, tx pgx.Tx, processID uuid.UUID) ([]ExtractFileRef, error) {
	rows, err := tx.Query(ctx, `
		SELECT f.id, COALESCE(f.doc_stage, ''), COALESCE(f.discipline, ''),
		       COALESCE(array_agg(pj.layout_key ORDER BY pj.page_from NULLS FIRST)
		                FILTER (WHERE pj.layout_key IS NOT NULL), '{}')
		FROM files f
		LEFT JOIN parse_jobs pj ON pj.file_id = f.id AND pj.status = 'DONE'
		WHERE f.process_id = $1 AND f.check_status = 'ACCEPTED' AND f.is_current = true
		GROUP BY f.id, f.uploaded_at
		ORDER BY f.uploaded_at
	`, processID)
	if err != nil {
		return nil, fmt.Errorf("query current accepted files: %w", err)
	}
	defer rows.Close()

	var out []ExtractFileRef
	for rows.Next() {
		var ref ExtractFileRef
		if err := rows.Scan(&ref.FileID, &ref.Stage, &ref.Discipline, &ref.LayoutKeys); err != nil {
			return nil, fmt.Errorf("scan file ref: %w", err)
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func (h *Handler) handleExtractCompleted(ctx context.Context, env events.Envelope) error {
	_, err := idempotency.Once(ctx, h.database, consumerName, env.EventID, func(tx pgx.Tx) error {
		var payload ExtractCompletedPayload
		if err := env.DecodeData(&payload); err != nil {
			return err
		}
		jobID, err := uuid.Parse(payload.JobID)
		if err != nil {
			return fmt.Errorf("parse job_id: %w", err)
		}
		processID, err := uuid.Parse(env.ProcessID)
		if err != nil {
			return fmt.Errorf("parse process_id: %w", err)
		}
		objectID, err := uuid.Parse(env.ObjectID)
		if err != nil {
			return fmt.Errorf("parse object_id: %w", err)
		}

		var facts []Fact
		if payload.FactsKey != "" { // пусто — воркер не нашёл фактов или упал (см. Error)
			facts, err = h.downloadFacts(ctx, payload.FactsKey)
			if err != nil {
				return fmt.Errorf("download facts %s: %w", payload.FactsKey, err)
			}
		}
		facts = filterValid(facts)

		for _, f := range facts {
			if err := insertFact(ctx, tx, processID, objectID, f); err != nil {
				return err
			}
		}
		if err := ProcessFacts(ctx, tx, processID, objectID, facts, h.ruleSet); err != nil {
			return err
		}

		status := "DONE"
		if payload.Error != "" {
			slog.Warn("extract job failed on worker side", "job_id", jobID, "error", payload.Error)
			status = "FAILED"
		}
		if _, err := tx.Exec(ctx, `UPDATE extract_jobs SET status = $2, finished_at = now() WHERE id = $1`, jobID, status); err != nil {
			return fmt.Errorf("update extract_job: %w", err)
		}

		return h.fanInToReady(ctx, tx, processID, objectID)
	})
	return err
}

// fanInToReady — когда все extract_jobs процесса завершены, переводит процесс PARSING -> READY и
// создаёт первую версию протокола (backend-plan.md §8.3, §8.7).
func (h *Handler) fanInToReady(ctx context.Context, tx pgx.Tx, processID, objectID uuid.UUID) error {
	var pending int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM extract_jobs WHERE process_id = $1 AND status NOT IN ('DONE', 'FAILED')
	`, processID).Scan(&pending); err != nil {
		return fmt.Errorf("count pending extract_jobs: %w", err)
	}
	if pending > 0 {
		return nil
	}

	tag, err := tx.Exec(ctx, `UPDATE processes SET status = 'READY', updated_at = now() WHERE id = $1 AND status = 'PARSING'`, processID)
	if err != nil {
		return fmt.Errorf("update process to READY: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil // уже READY (повторный fan-in по гонке) — не создавать вторую версию протокола
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO protocols (id, process_id, object_id, version, status)
		VALUES ($1, $2, $3, COALESCE((SELECT max(version) FROM protocols WHERE process_id = $2), 0) + 1, 'DRAFT')
	`, uuid.New(), processID, objectID); err != nil {
		return fmt.Errorf("insert protocol: %w", err)
	}
	return nil
}

func (h *Handler) downloadFacts(ctx context.Context, key string) ([]Fact, error) {
	obj, err := h.minio.Raw().GetObject(ctx, h.minio.BucketName(), key, miniogo.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}
	defer func() { _ = obj.Close() }()

	var facts []Fact
	scanner := bufio.NewScanner(obj)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var f Fact
		if err := json.Unmarshal(line, &f); err != nil {
			return nil, fmt.Errorf("decode fact line: %w", err)
		}
		facts = append(facts, f)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan facts jsonl: %w", err)
	}
	return facts, nil
}

func insertFact(ctx context.Context, tx pgx.Tx, processID, objectID uuid.UUID, f Fact) error {
	fileID, err := uuid.Parse(f.FileID)
	if err != nil {
		return fmt.Errorf("parse fact file_id %q: %w", f.FileID, err)
	}

	factID, err := uuid.Parse(f.FactID)
	if err != nil {
		factID = uuid.New()
	}

	bbox := []float64{f.BBox[0], f.BBox[1], f.BBox[2], f.BBox[3]}

	// value_norm — jsonb; пустое значение должно стать SQL NULL, а не невалидным jsonb.
	var valueNorm any
	if len(f.ValueNorm) > 0 && string(f.ValueNorm) != "null" {
		valueNorm = []byte(f.ValueNorm)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO facts (id, process_id, object_id, file_id, file_sha256, stage, param_code, rule_code,
			element_key, value_raw, value_norm, unit, page, bbox, quote, confidence, method, extractor_version,
			has_change_notice)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	`, factID, processID, objectID, fileID, nullIfEmpty(f.FileSHA256), f.Stage, nullIfEmpty(f.ParamCode),
		nullIfEmpty(f.RuleCode), nullIfEmpty(f.ElementKey), f.ValueRaw, valueNorm, nullIfEmpty(f.Unit), f.Page, bbox,
		f.Quote, f.Confidence, nullIfEmpty(f.Method), nullIfEmpty(f.ExtractorVersion), f.HasChangeNotice)
	if err != nil {
		return fmt.Errorf("insert fact: %w", err)
	}
	return nil
}

// ValidateFact — проверки backend-plan.md §9.1: без цитаты факт не доказательство; bbox — либо
// отсутствует (нулевой), либо нормализован в [0;1] и не вырожден. Номер страницы против page_count
// файла проверяется отдельно (нужен доступ к files, здесь только сам факт).
func ValidateFact(f Fact) error {
	if strings.TrimSpace(f.Quote) == "" {
		return errors.New("нет quote")
	}
	if f.Stage != "PD" && f.Stage != "RD" && f.Stage != "ID" {
		return fmt.Errorf("недопустимая стадия %q", f.Stage)
	}
	if f.ParamCode == "" {
		return errors.New("нет param_code")
	}
	if f.Page < 0 {
		return fmt.Errorf("отрицательная страница %d", f.Page)
	}
	if f.BBox == [4]float64{} {
		return nil
	}
	for _, v := range f.BBox {
		if v < 0 || v > 1 {
			return fmt.Errorf("bbox %v вне [0;1]", f.BBox)
		}
	}
	if f.BBox[0] >= f.BBox[2] || f.BBox[1] >= f.BBox[3] {
		return fmt.Errorf("вырожденный bbox %v", f.BBox)
	}
	return nil
}

func filterValid(facts []Fact) []Fact {
	out := facts[:0:0]
	for _, f := range facts {
		if err := ValidateFact(f); err != nil {
			slog.Warn("fact rejected", "fact_id", f.FactID, "param_code", f.ParamCode, "reason", err.Error())
			continue
		}
		out = append(out, f)
	}
	return out
}

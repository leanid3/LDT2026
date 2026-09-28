package process

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/audit"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/outbox"
)

var (
	ErrInvalidTransition    = errors.New("process: недопустимый переход статуса")
	ErrConflict             = errors.New("process: операция недопустима в текущем статусе")
	ErrHasPendingCandidates = errors.New("process: есть нерешённые CANDIDATE")
	ErrReasonRequired       = errors.New("process: для отмены финализации обязательна причина")
	ErrNoAcceptedFiles      = errors.New("process: нет ACCEPTED+is_current файлов для запуска проверки")
)

// CurrentFile — минимум, нужный process для нарезки заданий парсинга. Отдельный тип (а не files.File)
// — чтобы избежать цикла импорта files<->process (docs/architecture.md, паттерн инверсии зависимостей).
type CurrentFile struct {
	ID          uuid.UUID
	DocStage    string
	Discipline  string
	IsPDF       bool
	PageCount   int
	StorageKey  string
	SHA256      string
	ContentType string
}

type FileLister interface {
	ListCurrentAccepted(ctx context.Context, processID uuid.UUID) ([]CurrentFile, error)
}

// FindingsGate — то, что process.Finalize ожидает от пакета findings (структурно, без импорта).
type FindingsGate interface {
	HasPendingCandidates(ctx context.Context, tx pgx.Tx, processID uuid.UUID) (bool, error)
}

type Service struct {
	database *db.DB
	files    FileLister
	findings FindingsGate
}

func NewService(database *db.DB, files FileLister, findings FindingsGate) *Service {
	return &Service{database: database, files: files, findings: findings}
}

// EnsureUploadable — используется пакетом files при upload: создаёт новый процесс (processID == nil)
// либо проверяет, что существующий процесс допускает дозагрузку (backend-plan.md §4.2: разрешена в
// PENDING/READY/VERIFYING/COMPLETED, запрещена в PARSING/FINALIZED). Работает в переданной транзакции —
// вызывающий (files.Service) сам владеет границей транзакции всей операции upload.
func (s *Service) EnsureUploadable(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, processID *uuid.UUID, actorID uuid.UUID) (uuid.UUID, error) {
	repo := NewRepository(tx)

	if processID == nil {
		p, err := repo.Create(ctx, Process{ID: uuid.New(), ObjectID: objectID, Status: StatusPending, CreatedBy: &actorID})
		if err != nil {
			return uuid.Nil, err
		}
		return p.ID, nil
	}

	p, err := repo.GetForUpdate(ctx, *processID)
	if err != nil {
		return uuid.Nil, err
	}
	if p.Status == StatusParsing || p.Status == StatusFinalized {
		return uuid.Nil, fmt.Errorf("%w: процесс в статусе %s не допускает дозагрузку", ErrConflict, p.Status)
	}
	return p.ID, nil
}

// UpdateScenario — используется files.Service после confirm/registry, когда известен набор текущих
// стадий (backend-plan.md §8.4).
func (s *Service) UpdateScenario(ctx context.Context, tx pgx.Tx, processID uuid.UUID, scenario string) error {
	if scenario == "" {
		return nil
	}
	return NewRepository(tx).UpdateScenario(ctx, processID, scenario)
}

// ComputeScenario — backend-plan.md §8.4: по набору стадий среди текущих файлов.
func ComputeScenario(hasPD, hasRD, hasID, anyStagePartial bool) string {
	switch {
	case !hasPD && !hasRD && !hasID:
		return ""
	case anyStagePartial:
		return ScenarioPartiallyLoaded
	case hasPD && hasRD && hasID:
		return ScenarioFull
	case hasPD && hasRD:
		return ScenarioPDRDOnly
	case hasPD && hasID:
		return ScenarioPDIDOnly
	case hasRD && hasID:
		return ScenarioRDIDOnly
	default:
		return ScenarioSingleOnly
	}
}

// jobRange — страничный диапазон одного parse_job; From/To == nil для DOCX/XML (одно задание на файл).
type jobRange struct {
	From, To *int
}

const pagesPerParseJob = 20

// planParseJobs — нарезка PDF на задания по 20 страниц; DOCX/XML — одно задание (backend-plan.md §8.3).
func planParseJobs(f CurrentFile) []jobRange {
	if !f.IsPDF || f.PageCount <= 0 {
		return []jobRange{{}}
	}
	var out []jobRange
	for start := 1; start <= f.PageCount; start += pagesPerParseJob {
		end := start + pagesPerParseJob - 1
		if end > f.PageCount {
			end = f.PageCount
		}
		s, e := start, end
		out = append(out, jobRange{From: &s, To: &e})
	}
	return out
}

// Start переводит процесс PENDING -> PARSING: нарезает parse_jobs по каждому ACCEPTED+is_current
// файлу и пишет outbox doc.parse.requested (backend-plan.md §8.3).
func (s *Service) Start(ctx context.Context, processID uuid.UUID) (Process, error) {
	var result Process
	err := s.database.WithTx(ctx, func(tx pgx.Tx) error {
		repo := NewRepository(tx)

		p, err := repo.GetForUpdate(ctx, processID)
		if err != nil {
			return err
		}
		if p.Status != StatusPending {
			return fmt.Errorf("%w: процесс должен быть в PENDING, сейчас %s", ErrInvalidTransition, p.Status)
		}

		currentFiles, err := s.files.ListCurrentAccepted(ctx, processID)
		if err != nil {
			return fmt.Errorf("list current accepted files: %w", err)
		}
		if len(currentFiles) == 0 {
			return ErrNoAcceptedFiles
		}

		for _, f := range currentFiles {
			for _, jr := range planParseJobs(f) {
				jobID := uuid.New()
				if _, err := tx.Exec(ctx, `
					INSERT INTO parse_jobs (id, process_id, file_id, page_from, page_to, status)
					VALUES ($1, $2, $3, $4, $5, 'PENDING')
				`, jobID, processID, f.ID, jr.From, jr.To); err != nil {
					return fmt.Errorf("insert parse_job: %w", err)
				}

				if err := outbox.Insert(ctx, tx, outbox.Event{
					AggregateID: processID, EventType: "doc.parse.requested", Topic: "doc.parse.requested",
					MsgKey: p.ObjectID.String(), ObjectID: p.ObjectID.String(), ProcessID: processID.String(),
					// Поля — по contracts/events/doc.parse.requested.schema.json: воркер не ходит в БД
					// оркестратора, всё нужное для парсинга лежит в событии.
					Data: map[string]any{
						"job_id": jobID, "file_id": f.ID, "doc_stage": f.DocStage, "discipline": nullIfEmpty(f.Discipline),
						"sha256": nullIfEmpty(f.SHA256), "storage_key": f.StorageKey, "content_type": nullIfEmpty(f.ContentType),
						"page_from": jr.From, "page_to": jr.To,
					},
				}); err != nil {
					return fmt.Errorf("outbox doc.parse.requested: %w", err)
				}
			}
		}

		if err := repo.UpdateStatus(ctx, processID, StatusParsing); err != nil {
			return err
		}
		result, err = repo.Get(ctx, processID)
		return err
	})
	return result, err
}

// Finalize — backend-plan.md §4.2, §8.8: только из COMPLETED, только если нет CANDIDATE с
// inspector_status=PENDING; пишет outbox rin.sync.requested и переводит протокол в PENDING_SYNC.
func (s *Service) Finalize(ctx context.Context, processID, actorID uuid.UUID) (Process, error) {
	var result Process
	err := s.database.WithTx(ctx, func(tx pgx.Tx) error {
		repo := NewRepository(tx)

		p, err := repo.GetForUpdate(ctx, processID)
		if err != nil {
			return err
		}
		if p.Status != StatusCompleted {
			return fmt.Errorf("%w: процесс должен быть в COMPLETED, сейчас %s", ErrInvalidTransition, p.Status)
		}

		pending, err := s.findings.HasPendingCandidates(ctx, tx, processID)
		if err != nil {
			return fmt.Errorf("check pending candidates: %w", err)
		}
		if pending {
			return ErrHasPendingCandidates
		}

		if err := repo.Finalize(ctx, processID, actorID); err != nil {
			return err
		}

		if err := audit.Log(ctx, tx, audit.Entry{
			UserID: actorID, Action: "process.finalize", ObjectType: "process", ObjectID: processID.String(),
		}); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE protocols SET sync_status = 'PENDING_SYNC'
			WHERE process_id = $1 AND version = (SELECT max(version) FROM protocols WHERE process_id = $1)
		`, processID); err != nil {
			return fmt.Errorf("update protocol sync_status: %w", err)
		}

		if err := outbox.Insert(ctx, tx, outbox.Event{
			AggregateID: processID, EventType: "rin.sync.requested", Topic: "rin.sync.requested",
			MsgKey: p.ObjectID.String(), ObjectID: p.ObjectID.String(), ProcessID: processID.String(),
			Data: map[string]any{"process_id": processID},
		}); err != nil {
			return fmt.Errorf("outbox rin.sync.requested: %w", err)
		}

		result, err = repo.Get(ctx, processID)
		return err
	})
	return result, err
}

// Unfinalize — только вызывается после проверки роли на HTTP-уровне (supervisor/admin,
// backend/CLAUDE.md/backend-plan.md §4.2); reason обязателен и идёт в аудит.
func (s *Service) Unfinalize(ctx context.Context, processID, actorID uuid.UUID, reason string) (Process, error) {
	if reason == "" {
		return Process{}, ErrReasonRequired
	}

	var result Process
	err := s.database.WithTx(ctx, func(tx pgx.Tx) error {
		repo := NewRepository(tx)

		p, err := repo.GetForUpdate(ctx, processID)
		if err != nil {
			return err
		}
		if p.Status != StatusFinalized {
			return fmt.Errorf("%w: процесс должен быть в FINALIZED, сейчас %s", ErrInvalidTransition, p.Status)
		}

		if err := repo.Unfinalize(ctx, processID, StatusCompleted); err != nil {
			return err
		}

		if err := audit.Log(ctx, tx, audit.Entry{
			UserID: actorID, Action: "process.unfinalize", ObjectType: "process", ObjectID: processID.String(),
			Details: map[string]string{"reason": reason},
		}); err != nil {
			return err
		}

		result, err = repo.Get(ctx, processID)
		return err
	})
	return result, err
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Process, error) {
	return NewRepository(s.database.Pool()).Get(ctx, id)
}

func (s *Service) UploadStatuses(ctx context.Context, id uuid.UUID) ([]UploadStatusEntry, error) {
	return NewRepository(s.database.Pool()).UploadStatuses(ctx, id)
}

// nullIfEmpty — пустая строка в JSON события должна быть null, а не "" (contracts/events).
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *Service) ListByObject(ctx context.Context, objectID uuid.UUID) ([]Process, error) {
	return NewRepository(s.database.Pool()).ListByObject(ctx, objectID)
}

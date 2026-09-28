package files

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/process"
)

var (
	ErrNotFound        = errors.New("files: файл не найден")
	ErrNotDownloadable = errors.New("files: файл недоступен для скачивания")
)

type Repository struct {
	q db.Querier
}

func NewRepository(q db.Querier) *Repository {
	return &Repository{q: q}
}

// CreateUploading создаёт запись файла со статусом UPLOADING (шаг upload, backend-plan.md §8.1.1).
func (r *Repository) CreateUploading(ctx context.Context, f File) error {
	_, err := r.q.Exec(ctx, `
		INSERT INTO files (id, object_id, process_id, original_name, storage_key, size_bytes, check_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, f.ID, f.ObjectID, f.ProcessID, f.OriginalName, f.StorageKey, f.SizeBytes, CheckStatusUploading)
	if err != nil {
		return fmt.Errorf("insert file: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (File, error) {
	return r.scanOne(ctx, `SELECT `+fileColumns+` FROM files WHERE id = $1`, id)
}

// FindBySHA256InProcess — дедупликация по sha256 в рамках процесса (backend-plan.md §8.1.3).
func (r *Repository) FindBySHA256InProcess(ctx context.Context, processID uuid.UUID, sha256 string) (File, error) {
	return r.scanOne(ctx, `SELECT `+fileColumns+` FROM files WHERE process_id = $1 AND sha256 = $2`, processID, sha256)
}

func (r *Repository) ListByProcess(ctx context.Context, processID uuid.UUID) ([]File, error) {
	rows, err := r.q.Query(ctx, `SELECT `+fileColumns+` FROM files WHERE process_id = $1 ORDER BY uploaded_at`, processID)
	if err != nil {
		return nil, fmt.Errorf("query files: %w", err)
	}
	defer rows.Close()

	var out []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListCurrentAccepted — файлы готовые для запуска проверки: ACCEPTED + is_current=true. Реализует
// process.FileLister структурно (без импорта пакета process — см. docs/architecture.md).
func (r *Repository) ListCurrentAccepted(ctx context.Context, processID uuid.UUID) ([]File, error) {
	rows, err := r.q.Query(ctx, `
		SELECT `+fileColumns+` FROM files
		WHERE process_id = $1 AND check_status = $2 AND is_current = true
		ORDER BY uploaded_at
	`, processID, CheckStatusAccepted)
	if err != nil {
		return nil, fmt.Errorf("query current accepted files: %w", err)
	}
	defer rows.Close()

	var out []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UpdateCheckResult — итог confirm (backend-plan.md §8.1.2): sha256, content_type, page_count и
// финальный check_status/check_error.
func (r *Repository) UpdateCheckResult(ctx context.Context, id uuid.UUID, sha256, contentType *string, pageCount *int, status string, checkErr *string) error {
	_, err := r.q.Exec(ctx, `
		UPDATE files SET sha256 = $2, content_type = $3, page_count = $4, check_status = $5, check_error = $6
		WHERE id = $1
	`, id, sha256, contentType, pageCount, status, checkErr)
	if err != nil {
		return fmt.Errorf("update file check result: %w", err)
	}
	return nil
}

// UpdateRegistryFields — сопоставление с реестром (doc_stage/discipline/... и связи ревизий).
func (r *Repository) UpdateRegistryFields(ctx context.Context, id uuid.UUID, row RegistryRow, predecessorID, successorID *uuid.UUID) error {
	_, err := r.q.Exec(ctx, `
		UPDATE files SET
			doc_stage = $2, discipline = $3, document_code = $4, revision = $5,
			approval_status = $6, sheet_page_range = $7, signature_status = $8,
			predecessor_id = $9, successor_id = $10, approval_date = $11::date
		WHERE id = $1
	`, id, nullIfEmpty(row.DocStage), nullIfEmpty(row.Discipline), nullIfEmpty(row.DocumentCode),
		nullIfEmpty(row.Revision), nullIfEmpty(row.ApprovalStatus), nullIfEmpty(row.SheetPageRange),
		nullIfEmpty(row.SignatureStatus), predecessorID, successorID, nullIfEmpty(row.ApprovalDate))
	if err != nil {
		return fmt.Errorf("update file registry fields: %w", err)
	}
	return nil
}

// UpdateSelection — результат алгоритма выбора редакции (SelectCurrent).
func (r *Repository) UpdateSelection(ctx context.Context, id uuid.UUID, isCurrent bool, status, reason string) error {
	_, err := r.q.Exec(ctx, `
		UPDATE files SET is_current = $2, selection_status = $3, selection_reason = $4 WHERE id = $1
	`, id, isCurrent, nullIfEmpty(status), nullIfEmpty(reason))
	if err != nil {
		return fmt.Errorf("update file selection: %w", err)
	}
	return nil
}

// UpsertUploadStatus — статус загрузки по стадии (backend-plan.md §8.2, последний абзац).
func (r *Repository) UpsertUploadStatus(ctx context.Context, processID uuid.UUID, stage, status string) error {
	_, err := r.q.Exec(ctx, `
		INSERT INTO upload_status (process_id, stage, status) VALUES ($1, $2, $3)
		ON CONFLICT (process_id, stage) DO UPDATE SET status = EXCLUDED.status
	`, processID, stage, status)
	if err != nil {
		return fmt.Errorf("upsert upload_status: %w", err)
	}
	return nil
}

const fileColumns = `
	id, object_id, process_id, original_name, storage_key, size_bytes, sha256, content_type, page_count,
	check_status, check_error, doc_stage, discipline, document_code, revision, approval_status,
	approval_date, sheet_page_range, predecessor_id, successor_id, signature_status,
	is_current, selection_status, selection_reason, uploaded_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanFile(row rowScanner) (File, error) {
	var f File
	err := row.Scan(
		&f.ID, &f.ObjectID, &f.ProcessID, &f.OriginalName, &f.StorageKey, &f.SizeBytes, &f.SHA256, &f.ContentType, &f.PageCount,
		&f.CheckStatus, &f.CheckError, &f.DocStage, &f.Discipline, &f.DocumentCode, &f.Revision, &f.ApprovalStatus,
		&f.ApprovalDate, &f.SheetPageRange, &f.PredecessorID, &f.SuccessorID, &f.SignatureStatus,
		&f.IsCurrent, &f.SelectionStatus, &f.SelectionReason, &f.UploadedAt,
	)
	if err != nil {
		return File{}, fmt.Errorf("scan file: %w", err)
	}
	return f, nil
}

func (r *Repository) scanOne(ctx context.Context, query string, args ...any) (File, error) {
	f, err := scanFile(r.q.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, ErrNotFound
	}
	if err != nil {
		return File{}, err
	}
	return f, nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ProcessObjectID — объект, которому принадлежит процесс; process.ErrNotFound, если процесса нет.
func (r *Repository) ProcessObjectID(ctx context.Context, processID uuid.UUID) (uuid.UUID, error) {
	var objectID uuid.UUID
	err := r.q.QueryRow(ctx, `SELECT object_id FROM processes WHERE id = $1`, processID).Scan(&objectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, process.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("query process object: %w", err)
	}
	return objectID, nil
}

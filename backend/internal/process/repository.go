package process

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
)

var ErrNotFound = errors.New("process: процесс не найден")

type Repository struct {
	q db.Querier
}

func NewRepository(q db.Querier) *Repository {
	return &Repository{q: q}
}

func (r *Repository) Create(ctx context.Context, p Process) (Process, error) {
	err := r.q.QueryRow(ctx, `
		INSERT INTO processes (id, object_id, status, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at, updated_at
	`, p.ID, p.ObjectID, p.Status, p.CreatedBy).Scan(&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return Process{}, fmt.Errorf("insert process: %w", err)
	}
	return p, nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (Process, error) {
	var p Process
	err := r.q.QueryRow(ctx, `
		SELECT id, object_id, status, scenario, matrix_version, created_by, created_at, updated_at, finalized_at, finalized_by
		FROM processes WHERE id = $1
	`, id).Scan(&p.ID, &p.ObjectID, &p.Status, &p.Scenario, &p.MatrixVersion, &p.CreatedBy,
		&p.CreatedAt, &p.UpdatedAt, &p.FinalizedAt, &p.FinalizedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return Process{}, ErrNotFound
	}
	if err != nil {
		return Process{}, fmt.Errorf("query process: %w", err)
	}
	return p, nil
}

// GetForUpdate — блокирует строку процесса на время транзакции (переходы состояний должны быть
// сериализованы, чтобы конкурентные start/finalize не гонялись).
func (r *Repository) GetForUpdate(ctx context.Context, id uuid.UUID) (Process, error) {
	var p Process
	err := r.q.QueryRow(ctx, `
		SELECT id, object_id, status, scenario, matrix_version, created_by, created_at, updated_at, finalized_at, finalized_by
		FROM processes WHERE id = $1 FOR UPDATE
	`, id).Scan(&p.ID, &p.ObjectID, &p.Status, &p.Scenario, &p.MatrixVersion, &p.CreatedBy,
		&p.CreatedAt, &p.UpdatedAt, &p.FinalizedAt, &p.FinalizedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return Process{}, ErrNotFound
	}
	if err != nil {
		return Process{}, fmt.Errorf("query process for update: %w", err)
	}
	return p, nil
}

func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.q.Exec(ctx, `UPDATE processes SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("update process status: %w", err)
	}
	return nil
}

func (r *Repository) UpdateScenario(ctx context.Context, id uuid.UUID, scenario string) error {
	_, err := r.q.Exec(ctx, `UPDATE processes SET scenario = $2, updated_at = now() WHERE id = $1`, id, scenario)
	if err != nil {
		return fmt.Errorf("update process scenario: %w", err)
	}
	return nil
}

func (r *Repository) Finalize(ctx context.Context, id uuid.UUID, by uuid.UUID) error {
	_, err := r.q.Exec(ctx, `
		UPDATE processes SET status = $2, finalized_at = now(), finalized_by = $3, updated_at = now() WHERE id = $1
	`, id, StatusFinalized, by)
	if err != nil {
		return fmt.Errorf("finalize process: %w", err)
	}
	return nil
}

func (r *Repository) Unfinalize(ctx context.Context, id uuid.UUID, status string) error {
	_, err := r.q.Exec(ctx, `
		UPDATE processes SET status = $2, finalized_at = NULL, finalized_by = NULL, updated_at = now() WHERE id = $1
	`, id, status)
	if err != nil {
		return fmt.Errorf("unfinalize process: %w", err)
	}
	return nil
}

func (r *Repository) UploadStatuses(ctx context.Context, processID uuid.UUID) ([]UploadStatusEntry, error) {
	rows, err := r.q.Query(ctx, `SELECT stage, status FROM upload_status WHERE process_id = $1 ORDER BY stage`, processID)
	if err != nil {
		return nil, fmt.Errorf("query upload_status: %w", err)
	}
	defer rows.Close()

	var out []UploadStatusEntry
	for rows.Next() {
		var e UploadStatusEntry
		if err := rows.Scan(&e.Stage, &e.Status); err != nil {
			return nil, fmt.Errorf("scan upload_status: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListByObject — процессы объекта, новые сверху.
func (r *Repository) ListByObject(ctx context.Context, objectID uuid.UUID) ([]Process, error) {
	rows, err := r.q.Query(ctx, `
		SELECT id, object_id, status, scenario, matrix_version, created_by, created_at, updated_at, finalized_at, finalized_by
		FROM processes WHERE object_id = $1 ORDER BY created_at DESC
	`, objectID)
	if err != nil {
		return nil, fmt.Errorf("query processes by object: %w", err)
	}
	defer rows.Close()

	var out []Process
	for rows.Next() {
		var p Process
		if err := rows.Scan(&p.ID, &p.ObjectID, &p.Status, &p.Scenario, &p.MatrixVersion, &p.CreatedBy,
			&p.CreatedAt, &p.UpdatedAt, &p.FinalizedAt, &p.FinalizedBy); err != nil {
			return nil, fmt.Errorf("scan process: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

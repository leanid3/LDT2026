package protocol

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
)

var ErrNotFound = errors.New("protocol: протокол не найден")

type Repository struct {
	q db.Querier
}

func NewRepository(q db.Querier) *Repository {
	return &Repository{q: q}
}

// Create — новая версия протокола (backend-plan.md §8.7); версия — следующая по счёту для процесса.
func (r *Repository) Create(ctx context.Context, p Protocol) (Protocol, error) {
	err := r.q.QueryRow(ctx, `
		INSERT INTO protocols (id, process_id, object_id, version, status, matrix_version, dataset_version, model_version)
		VALUES ($1, $2, $3,
			COALESCE((SELECT max(version) FROM protocols WHERE process_id = $2), 0) + 1,
			$4, $5, $6, $7)
		RETURNING version, created_at
	`, p.ID, p.ProcessID, p.ObjectID, p.Status, p.MatrixVersion, p.DatasetVersion, p.ModelVersion).
		Scan(&p.Version, &p.CreatedAt)
	if err != nil {
		return Protocol{}, fmt.Errorf("insert protocol: %w", err)
	}
	return p, nil
}

func (r *Repository) LatestByProcess(ctx context.Context, processID uuid.UUID) (Protocol, error) {
	var p Protocol
	err := r.q.QueryRow(ctx, `
		SELECT id, process_id, object_id, version, status, matrix_version, dataset_version, model_version, created_at, finalized_at, sync_status
		FROM protocols WHERE process_id = $1 ORDER BY version DESC LIMIT 1
	`, processID).Scan(&p.ID, &p.ProcessID, &p.ObjectID, &p.Version, &p.Status,
		&p.MatrixVersion, &p.DatasetVersion, &p.ModelVersion, &p.CreatedAt, &p.FinalizedAt, &p.SyncStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Protocol{}, ErrNotFound
	}
	if err != nil {
		return Protocol{}, fmt.Errorf("query latest protocol: %w", err)
	}
	return p, nil
}

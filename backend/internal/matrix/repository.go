package matrix

import (
	"context"
	"fmt"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
)

// Repository — чтение каталога параметров из params (наполняется import-matrix).
type Repository struct {
	q db.Querier
}

func NewRepository(q db.Querier) *Repository { return &Repository{q: q} }

// ListActive — активные параметры в порядке кодов и версия Матрицы (максимальная среди них).
func (r *Repository) ListActive(ctx context.Context) (version string, params []Param, err error) {
	rows, err := r.q.Query(ctx, `
		SELECT code, COALESCE(section, ''), parameter_name, COALESCE(unit, ''), COALESCE(review_priority, ''),
			COALESCE(data_type, ''), COALESCE(source_pd, ''), COALESCE(source_rd, ''), COALESCE(source_id, ''),
			COALESCE(trigger_logic, ''), matrix_version
		FROM params WHERE is_active ORDER BY code
	`)
	if err != nil {
		return "", nil, fmt.Errorf("query params: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p Param
		var v string
		if err := rows.Scan(&p.Code, &p.Section, &p.Name, &p.Unit, &p.ReviewPriority, &p.DataType,
			&p.SourcePD, &p.SourceRD, &p.SourceID, &p.TriggerLogic, &v); err != nil {
			return "", nil, fmt.Errorf("scan param: %w", err)
		}
		if v > version {
			version = v
		}
		params = append(params, p)
	}
	return version, params, rows.Err()
}

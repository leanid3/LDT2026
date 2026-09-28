package findings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
)

var ErrNotFound = errors.New("findings: finding не найден")

type Repository struct {
	q db.Querier
}

func NewRepository(q db.Querier) *Repository {
	return &Repository{q: q}
}

const findingColumns = `id, check_id, parent_finding_id, finding_status, inspector_status, decided_by, decided_at, reason_code, comment, version`

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (Finding, error) {
	return r.scanOne(ctx, `SELECT `+findingColumns+` FROM findings WHERE id = $1`, id)
}

// GetForUpdate — блокирует строку на время транзакции решения (конкурентные decision должны быть
// сериализованы).
func (r *Repository) GetForUpdate(ctx context.Context, id uuid.UUID) (Finding, error) {
	return r.scanOne(ctx, `SELECT `+findingColumns+` FROM findings WHERE id = $1 FOR UPDATE`, id)
}

func (r *Repository) scanOne(ctx context.Context, query string, args ...any) (Finding, error) {
	var f Finding
	err := r.q.QueryRow(ctx, query, args...).Scan(
		&f.ID, &f.CheckID, &f.ParentFindingID, &f.FindingStatus, &f.InspectorStatus,
		&f.DecidedBy, &f.DecidedAt, &f.ReasonCode, &f.Comment, &f.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Finding{}, ErrNotFound
	}
	if err != nil {
		return Finding{}, fmt.Errorf("query finding: %w", err)
	}
	return f, nil
}

// ProcessAndStatusForCheck — процесс и его текущий статус для finding (через check_id -> checks.process_id).
func (r *Repository) ProcessAndStatusForCheck(ctx context.Context, checkID uuid.UUID) (processID uuid.UUID, processStatus string, err error) {
	err = r.q.QueryRow(ctx, `
		SELECT p.id, p.status FROM checks c JOIN processes p ON p.id = c.process_id WHERE c.id = $1
	`, checkID).Scan(&processID, &processStatus)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("lookup process for check: %w", err)
	}
	return processID, processStatus, nil
}

func (r *Repository) UpdateDecision(ctx context.Context, id uuid.UUID, inspectorStatus string, decidedBy uuid.UUID, reasonCode, comment *string) error {
	_, err := r.q.Exec(ctx, `
		UPDATE findings SET inspector_status = $2, decided_by = $3, decided_at = now(),
			reason_code = $4, comment = $5, version = version + 1
		WHERE id = $1
	`, id, inspectorStatus, decidedBy, reasonCode, comment)
	if err != nil {
		return fmt.Errorf("update finding decision: %w", err)
	}
	return nil
}

// CountPendingCandidates — CANDIDATE с inspector_status=PENDING в рамках процесса (через checks).
func (r *Repository) CountPendingCandidates(ctx context.Context, processID uuid.UUID) (int, error) {
	var count int
	err := r.q.QueryRow(ctx, `
		SELECT count(*) FROM findings f JOIN checks c ON c.id = f.check_id
		WHERE c.process_id = $1 AND f.finding_status = $2 AND f.inspector_status = $3
	`, processID, StatusCandidate, InspectorPending).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending candidates: %w", err)
	}
	return count, nil
}

const findingColumnsJoined = `f.id, f.check_id, f.parent_finding_id, f.finding_status, f.inspector_status, f.decided_by, f.decided_at, f.reason_code, f.comment, f.version`

// detailedSelect — finding вместе с тем, что нужно карточке: параметр Матрицы, значения сравнения и
// пояснение движка. LEFT JOIN: finding без группы/параметра (например, из старых данных) не пропадает.
const detailedSelect = `
	SELECT ` + findingColumnsJoined + `,
		eg.id, eg.param_code, p.parameter_name, p.unit, p.review_priority,
		c.expected_value, c.actual_value, c.delta, c.rationale
	FROM findings f
	JOIN checks c ON c.id = f.check_id
	LEFT JOIN evidence_groups eg ON eg.id = c.evidence_group_id
	LEFT JOIN params p ON p.code = eg.param_code`

func scanDetailed(row pgx.Row) (Finding, *uuid.UUID, error) {
	var f Finding
	var groupID *uuid.UUID
	err := row.Scan(
		&f.ID, &f.CheckID, &f.ParentFindingID, &f.FindingStatus, &f.InspectorStatus,
		&f.DecidedBy, &f.DecidedAt, &f.ReasonCode, &f.Comment, &f.Version,
		&groupID, &f.ParamCode, &f.ParameterName, &f.Unit, &f.ReviewPriority,
		&f.ExpectedValue, &f.ActualValue, &f.Delta, &f.Rationale,
	)
	return f, groupID, err
}

// GetDetailed — finding с карточкой и доказательствами (для GET /findings/{id} и ответа на решение).
func (r *Repository) GetDetailed(ctx context.Context, id uuid.UUID) (Finding, error) {
	f, groupID, err := scanDetailed(r.q.QueryRow(ctx, detailedSelect+` WHERE f.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Finding{}, ErrNotFound
	}
	if err != nil {
		return Finding{}, fmt.Errorf("query detailed finding: %w", err)
	}
	if groupID != nil {
		ev, err := r.evidenceByGroup(ctx, []uuid.UUID{*groupID})
		if err != nil {
			return Finding{}, err
		}
		f.Evidence = ev[*groupID]
	}
	return f, nil
}

// ListByProcess — все findings процесса с карточками (для протокола, internal/protocol). Порядок для
// работы инспектора: сначала то, что требует решения (CANDIDATE), внутри — по приоритету параметра.
func (r *Repository) ListByProcess(ctx context.Context, processID uuid.UUID) ([]Finding, error) {
	rows, err := r.q.Query(ctx, detailedSelect+`
		WHERE c.process_id = $1
		ORDER BY CASE f.finding_status
				WHEN 'CANDIDATE' THEN 0 WHEN 'CONFIRMED_VIOLATION' THEN 1 WHEN 'MISSING_EVIDENCE' THEN 2
				WHEN 'CLARIFICATION_REQUIRED' THEN 3 ELSE 4 END,
			CASE p.review_priority WHEN 'HIGH' THEN 0 WHEN 'MEDIUM' THEN 1 ELSE 2 END,
			eg.param_code, f.id
	`, processID)
	if err != nil {
		return nil, fmt.Errorf("query findings by process: %w", err)
	}
	defer rows.Close()

	var out []Finding
	var groups []*uuid.UUID
	for rows.Next() {
		f, groupID, err := scanDetailed(rows)
		if err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		out = append(out, f)
		groups = append(groups, groupID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var ids []uuid.UUID
	for _, g := range groups {
		if g != nil {
			ids = append(ids, *g)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	ev, err := r.evidenceByGroup(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i, g := range groups {
		if g != nil {
			out[i].Evidence = ev[*g]
		}
	}
	return out, nil
}

// evidenceByGroup — одним запросом доказательства всех групп (без N+1). Ожидаемая сторона идёт первой.
func (r *Repository) evidenceByGroup(ctx context.Context, groupIDs []uuid.UUID) (map[uuid.UUID][]Evidence, error) {
	rows, err := r.q.Query(ctx, `
		SELECT ef.evidence_group_id, ef.file_id, COALESCE(fl.original_name, ''), ef.stage, ef.sheet_page,
			ef.bbox_polygon_norm, ef.quote, ef.extracted_value, ef.role_expected_actual
		FROM evidence_fragments ef
		LEFT JOIN files fl ON fl.id = ef.file_id
		WHERE ef.evidence_group_id = ANY($1)
		ORDER BY ef.evidence_group_id, ef.role_expected_actual
	`, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("query evidence: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID][]Evidence)
	for rows.Next() {
		var (
			groupID uuid.UUID
			e       Evidence
			bbox    []byte
		)
		if err := rows.Scan(&groupID, &e.FileID, &e.OriginalName, &e.Stage, &e.Page, &bbox, &e.Quote, &e.ExtractedValue, &e.Role); err != nil {
			return nil, fmt.Errorf("scan evidence: %w", err)
		}
		if len(bbox) > 0 {
			var box []float64
			if json.Unmarshal(bbox, &box) == nil && len(box) == 4 {
				e.BBox = box
			}
		}
		out[groupID] = append(out[groupID], e)
	}
	return out, rows.Err()
}

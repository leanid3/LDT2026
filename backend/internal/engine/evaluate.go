package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine/rules"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/findings"
)

type groupKey struct {
	paramCode  string
	elementKey string
}

// ProcessFacts группирует факты по (param_code, element_key) и прогоняет через движок правил
// (backend-plan.md §8.5): применимость (есть ли правило) → сопоставимость (есть ли expected/actual) →
// сравнение. Пишет evidence_groups/evidence_fragments/checks/findings — одна транзакция на пачку
// фактов одного extract_job (вызывающий код оборачивает в db.WithTx).
func ProcessFacts(ctx context.Context, tx pgx.Tx, processID, objectID uuid.UUID, facts []Fact, ruleSet map[string]rules.Rule) error {
	groups := make(map[groupKey][]Fact)
	for _, f := range facts {
		k := groupKey{paramCode: f.ParamCode, elementKey: f.ElementKey}
		groups[k] = append(groups[k], f)
	}

	// Детерминированный порядок обработки — удобнее для тестов и логов.
	keys := make([]groupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].paramCode != keys[j].paramCode {
			return keys[i].paramCode < keys[j].paramCode
		}
		return keys[i].elementKey < keys[j].elementKey
	})

	for _, k := range keys {
		if err := processGroup(ctx, tx, processID, objectID, k, groups[k], ruleSet); err != nil {
			return err
		}
	}
	return nil
}

func processGroup(ctx context.Context, tx pgx.Tx, processID, objectID uuid.UUID, k groupKey, groupFacts []Fact, ruleSet map[string]rules.Rule) error {
	var expected, actual *Fact
	for i := range groupFacts {
		f := &groupFacts[i]
		switch f.Stage {
		case "PD":
			if expected == nil {
				expected = f
			}
		case "RD", "ID":
			if actual == nil {
				actual = f
			}
		}
	}

	rule, hasRule := ruleSet[k.paramCode]

	var result rules.Result
	switch {
	case !hasRule:
		result = rules.NotComparable()
	default:
		expectedRaw, actualRaw := "", ""
		if expected != nil {
			expectedRaw = expected.Comparable()
		}
		if actual != nil {
			actualRaw = actual.Comparable()
		}
		result = rules.Evaluate(rule, expectedRaw, actualRaw)
	}

	evidenceGroupID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO evidence_groups (id, process_id, object_id, param_code, rule_code, element_key, input_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, evidenceGroupID, processID, objectID, k.paramCode, nullIfEmpty(rule.Code), nullIfEmpty(k.elementKey),
		inputHash(groupFacts)); err != nil {
		return fmt.Errorf("insert evidence_group: %w", err)
	}

	if expected != nil {
		if err := insertFragment(ctx, tx, evidenceGroupID, *expected, "expected"); err != nil {
			return err
		}
	}
	if actual != nil {
		if err := insertFragment(ctx, tx, evidenceGroupID, *actual, "actual"); err != nil {
			return err
		}
	}

	checkID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO checks (id, process_id, object_id, evidence_group_id, expected_value, actual_value, delta,
			finding_status, rationale, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
	`, checkID, processID, objectID, evidenceGroupID, nullIfEmpty(result.ExpectedValue), nullIfEmpty(result.ActualValue),
		nullIfEmpty(result.Delta), result.FindingStatus, nullIfEmpty(result.Rationale)); err != nil {
		return fmt.Errorf("insert check: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO findings (id, check_id, finding_status, inspector_status)
		VALUES ($1, $2, $3, $4)
	`, uuid.New(), checkID, result.FindingStatus, findings.InspectorPending); err != nil {
		return fmt.Errorf("insert finding: %w", err)
	}

	return nil
}

func insertFragment(ctx context.Context, tx pgx.Tx, evidenceGroupID uuid.UUID, f Fact, role string) error {
	fileID, err := uuid.Parse(f.FileID)
	if err != nil {
		return fmt.Errorf("parse fact file_id %q: %w", f.FileID, err)
	}
	// bbox: нулевой означает «геометрии нет» (NULL), иначе [x0,y0,x1,y1] в [0;1] для подсветки в просмотрщике.
	var bbox any
	if f.BBox != ([4]float64{}) {
		raw, mErr := json.Marshal(f.BBox[:])
		if mErr != nil {
			return fmt.Errorf("marshal bbox: %w", mErr)
		}
		bbox = raw
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO evidence_fragments (id, evidence_group_id, file_id, stage, sheet_page, bbox_polygon_norm,
			extracted_value, quote, role_expected_actual)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, uuid.New(), evidenceGroupID, fileID, f.Stage, f.Page, bbox, f.ValueRaw, f.Quote, role)
	if err != nil {
		return fmt.Errorf("insert evidence_fragment: %w", err)
	}
	return nil
}

// inputHash — хеш входных фактов группы, нужен для инкрементального пересчёта в будущем (M2+,
// backend-plan.md §8.6); сейчас просто вычисляется и сохраняется, реального переиспользования ещё нет.
func inputHash(facts []Fact) string {
	h := sha256.New()
	ids := make([]string, len(facts))
	for i, f := range facts {
		ids[i] = f.FactID
	}
	sort.Strings(ids)
	for _, id := range ids {
		h.Write([]byte(id))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

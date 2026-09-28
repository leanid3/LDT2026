// Package matrix — чтение Матрицы параметров (xlsx, лист «МАТРИЦА») и загрузка её в таблицу params.
// Источник истины — файл заказчика docs/source/Матрица_параметров_редакция1.1.xlsx (ТЗ, 132 параметра).
package matrix

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine/rules"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
)

const SheetName = "МАТРИЦА"

// Param — одна строка Матрицы.
type Param struct {
	Code           string `json:"code"`
	Section        string `json:"section"`
	Name           string `json:"name"`
	Unit           string `json:"unit"`
	SourcePD       string `json:"source_pd"`
	SourceRD       string `json:"source_rd"`
	SourceID       string `json:"source_id"`
	TriggerLogic   string `json:"trigger_logic"`
	ReviewPriority string `json:"review_priority"` // HIGH | MEDIUM | LOW
	DataType       string `json:"data_type"`       // number | ordinal | string
	RuleType       string `json:"rule_type,omitempty"`
	Scale          string `json:"scale,omitempty"`
}

var codeRe = regexp.MustCompile(`^M-\d{3}$`)

// header -> имя поля: колонки ищем по заголовку, а не по номеру, чтобы перестановка колонок в новой
// редакции Матрицы не ломала импорт молча.
var headerFields = map[string]string{
	"Код параметра":           "code",
	"Раздел ПД (ПП РФ № 87)":  "section",
	"Контролируемый параметр": "name",
	"Ед. изм.":                "unit",
	"Источник в ПД":           "source_pd",
	"Источник в РД":           "source_rd",
	"Источник в ИД":           "source_id",
	"Логика ИИ-связи (предварительный триггер)": "trigger",
	"Приоритет экспертной проверки":             "priority",
}

// ReadXLSX читает параметры из файла. Строки без кода M-NNN пропускаются; дубликаты кодов и
// отсутствие обязательных заголовков — ошибка.
func ReadXLSX(path string) ([]Param, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer func() { _ = f.Close() }()

	rows, err := f.GetRows(SheetName)
	if err != nil {
		return nil, fmt.Errorf("read sheet %q: %w", SheetName, err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("лист %q пуст", SheetName)
	}

	cols := map[string]int{}
	for i, h := range rows[0] {
		if field, ok := headerFields[strings.TrimSpace(h)]; ok {
			cols[field] = i
		}
	}
	for _, need := range []string{"code", "name"} {
		if _, ok := cols[need]; !ok {
			return nil, fmt.Errorf("в листе %q нет обязательной колонки %q", SheetName, need)
		}
	}
	get := func(row []string, field string) string {
		if i, ok := cols[field]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}

	seen := map[string]bool{}
	var out []Param
	for _, row := range rows[1:] {
		code := get(row, "code")
		if !codeRe.MatchString(code) {
			continue
		}
		if seen[code] {
			return nil, fmt.Errorf("дублирующийся код параметра %s", code)
		}
		seen[code] = true
		out = append(out, Param{
			Code: code, Section: get(row, "section"), Name: get(row, "name"), Unit: get(row, "unit"),
			SourcePD: get(row, "source_pd"), SourceRD: get(row, "source_rd"), SourceID: get(row, "source_id"),
			TriggerLogic: get(row, "trigger"), ReviewPriority: priority(get(row, "priority")),
		})
	}
	return out, nil
}

// priority: «HIGH — обязательная экспертная проверка» -> HIGH. Неизвестное значение -> "" (NULL в БД).
func priority(s string) string {
	up := strings.ToUpper(s)
	for _, p := range []string{"HIGH", "MEDIUM", "LOW"} {
		if strings.HasPrefix(up, p) {
			return p
		}
	}
	return ""
}

// Enrich проставляет data_type/rule_type/scale по каталогу правил: тип данных параметра определяется
// тем, как его сравнивают. Параметр без правила остаётся string — извлекать его можно, сравнивать пока нет.
func Enrich(params []Param, set map[string]rules.Rule) {
	for i := range params {
		r, ok := set[params[i].Code]
		if !ok {
			params[i].DataType = "string"
			continue
		}
		params[i].RuleType, params[i].Scale = r.Type, r.Scale
		switch r.Type {
		case rules.TypeOrdinalDecrease:
			params[i].DataType = "ordinal"
		case rules.TypeTextMismatch:
			params[i].DataType = "string"
		default:
			params[i].DataType = "number"
		}
	}
}

// Upsert загружает параметры в params транзакционно и идемпотентно (по code). Параметры, которых нет
// в файле, не удаляются, а деактивируются — на них могут ссылаться прошлые проверки.
func Upsert(ctx context.Context, database *db.DB, params []Param, version string) error {
	return database.WithTx(ctx, func(tx pgx.Tx) error {
		codes := make([]string, 0, len(params))
		for _, p := range params {
			codes = append(codes, p.Code)
			if _, err := tx.Exec(ctx, `
				INSERT INTO params (code, section, parameter_name, unit, source_pd, source_rd, source_id,
					trigger_logic, review_priority, data_type, is_active, matrix_version)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, true, $11)
				ON CONFLICT (code) DO UPDATE SET section = EXCLUDED.section, parameter_name = EXCLUDED.parameter_name,
					unit = EXCLUDED.unit, source_pd = EXCLUDED.source_pd, source_rd = EXCLUDED.source_rd,
					source_id = EXCLUDED.source_id, trigger_logic = EXCLUDED.trigger_logic,
					review_priority = EXCLUDED.review_priority, data_type = EXCLUDED.data_type,
					is_active = true, matrix_version = EXCLUDED.matrix_version, updated_at = now()
			`, p.Code, nullIfEmpty(p.Section), p.Name, nullIfEmpty(p.Unit), nullIfEmpty(p.SourcePD),
				nullIfEmpty(p.SourceRD), nullIfEmpty(p.SourceID), nullIfEmpty(p.TriggerLogic),
				nullIfEmpty(p.ReviewPriority), nullIfEmpty(p.DataType), version); err != nil {
				return fmt.Errorf("upsert %s: %w", p.Code, err)
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE params SET is_active = false, updated_at = now() WHERE NOT (code = ANY($1))`, codes); err != nil {
			return fmt.Errorf("deactivate stale params: %w", err)
		}
		return nil
	})
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

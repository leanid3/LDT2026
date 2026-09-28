// Package engine — оркестрация cmd/engine: fan-in/fan-out заданий парсинга/извлечения и движок
// проверок (backend-plan.md §8.3, §8.5). Контракт с воркерами — contracts/facts.schema.json и
// contracts/events/*.schema.json; Fact ниже обязан ему соответствовать (проверяется contract_test.go
// в internal/mockworkers на общих примерах contracts/events/examples).
package engine

import (
	"encoding/json"
	"strconv"
)

// Fact — один факт из JSONL (backend-plan.md §9.1), которые пишет LLM-воркер (или cmd/mockworkers).
type Fact struct {
	FactID           string          `json:"fact_id"`
	ProcessID        string          `json:"process_id"`
	FileID           string          `json:"file_id"`
	FileSHA256       string          `json:"file_sha256"`
	Stage            string          `json:"stage"` // PD | RD | ID
	ParamCode        string          `json:"param_code"`
	RuleCode         string          `json:"rule_code"`
	ElementKey       string          `json:"element_key"`
	ValueRaw         string          `json:"value_raw"`
	ValueNorm        json.RawMessage `json:"value_norm,omitempty"`
	Unit             string          `json:"unit"`
	Page             int             `json:"page"`
	BBox             [4]float64      `json:"bbox"`
	Quote            string          `json:"quote"`
	Confidence       float64         `json:"confidence"`
	Method           string          `json:"method"`
	Quality          string          `json:"quality"`
	HasChangeNotice  bool            `json:"has_change_notice"`
	ExtractorVersion string          `json:"extractor_version"`
}

// Comparable — значение, по которому факт сравнивается правилами: нормализованное value_norm.value
// (число/порядковое значение/строка), если воркер его дал, иначе value_raw как есть. Сырая строка
// («1 250,5 м²», «В30 W6») нужна инспектору в карточке, сравнивать надёжнее нормализованное.
func (f Fact) Comparable() string {
	if len(f.ValueNorm) == 0 {
		return f.ValueRaw
	}
	var n struct {
		Kind  string `json:"kind"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal(f.ValueNorm, &n); err != nil {
		return f.ValueRaw
	}
	switch v := n.Value.(type) {
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		if v != "" {
			return v
		}
	}
	return f.ValueRaw
}

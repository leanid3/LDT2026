// Package rules — механизм правил сравнения по backend-plan.md §8.5. Каждое правило — файл
// rules/<код параметра>.yaml, тип правила выбирает способ сравнения ожидаемого (эталон, обычно ПД) и
// фактического (РД/ИД) значений. Параметр без загруженного правила честно получает
// NOT_COMPARABLE/RULE_NOT_IMPLEMENTED (§8.5 п.1) — это не нарушение.
package rules

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Типы правил из backend-plan.md §8.5. Не реализованы (TODO(M3)): set_difference, presence, tolerance —
// им нужны множества/элементы и допуски из value_norm, дают NOT_COMPARABLE до реализации.
const (
	TypeNumericEqual    = "numeric_equal"     // расхождение > tolerance_pct (по умолчанию > 0)
	TypeNumericDeltaPct = "numeric_delta_pct" // |a−e|/|e| > pct
	TypeNumericDecrease = "numeric_decrease"  // значение в РД/ИД меньше ПД
	TypeNumericIncrease = "numeric_increase"  // значение в РД/ИД больше ПД (более чем на pct %, если задан)
	TypeThresholdMin    = "threshold_min"     // значение < абсолютного порога
	TypeThresholdMax    = "threshold_max"     // значение > абсолютного порога
	TypeOrdinalDecrease = "ordinal_decrease"  // понижение по упорядоченной шкале (scales.yaml)
	TypeTextMismatch    = "text_mismatch"     // нормализованные строки различаются
)

// ScalesFile — имя файла со шкалами в каталоге правил; не является правилом и пропускается в LoadDir.
const ScalesFile = "scales.yaml"

type Rule struct {
	Code           string  `yaml:"code"`
	Type           string  `yaml:"type"`
	Threshold      float64 `yaml:"threshold"`
	Pct            float64 `yaml:"pct"`
	ToleranceEqual float64 `yaml:"tolerance_pct"`
	Scale          string  `yaml:"scale"`
	Description    string  `yaml:"description"`
	NormativeBasis string  `yaml:"normative_basis"`
	scaleOrder     [][]string
}

// LoadDir читает все правила *.yaml из dir (кроме scales.yaml) в map[code]Rule и подставляет в
// ordinal_decrease-правила их шкалы. Ошибка в любом правиле — ошибка загрузки целиком: лучше не
// стартовать, чем молча работать с частью каталога.
func LoadDir(dir string) (map[string]Rule, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read rules dir: %w", err)
	}

	scales, err := loadScales(filepath.Join(dir, ScalesFile))
	if err != nil {
		return nil, err
	}

	out := make(map[string]Rule)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") || e.Name() == ScalesFile {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read rule file %s: %w", e.Name(), err)
		}
		var r Rule
		if err := yaml.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("parse rule file %s: %w", e.Name(), err)
		}
		if r.Code == "" {
			return nil, fmt.Errorf("rule file %s: пустой code", e.Name())
		}
		if r.Type == TypeOrdinalDecrease {
			order, ok := scales[r.Scale]
			if !ok {
				return nil, fmt.Errorf("rule %s: неизвестная шкала %q (см. %s)", r.Code, r.Scale, ScalesFile)
			}
			r.scaleOrder = order
		}
		out[r.Code] = r
	}
	return out, nil
}

// loadScales читает шкалы: имя -> список рангов от худшего к лучшему, ранг = алиасы через "|"
// (например "I|1"). Файла нет — шкал нет (ordinal_decrease-правила тогда не загрузятся).
func loadScales(path string) (map[string][][]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string][][]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read scales: %w", err)
	}
	var raw map[string][]string
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse scales: %w", err)
	}
	out := make(map[string][][]string, len(raw))
	for name, ranks := range raw {
		for _, rank := range ranks {
			var aliases []string
			for _, a := range strings.Split(rank, "|") {
				aliases = append(aliases, normalizeToken(a))
			}
			out[name] = append(out[name], aliases)
		}
	}
	return out, nil
}

// finding_status — свои строковые константы (а не import findings), чтобы не тянуть весь пакет
// findings в движок; значения идентичны backend-plan.md §4.1.
const (
	StatusCandidate        = "CANDIDATE"
	StatusNegativeVerified = "NEGATIVE_VERIFIED"
	StatusMissingEvidence  = "MISSING_EVIDENCE"
	StatusNotComparable    = "NOT_COMPARABLE"
)

type Result struct {
	FindingStatus string
	ExpectedValue string
	ActualValue   string
	Delta         string
	Rationale     string
}

// Evaluate сравнивает эталонное (expected, обычно ПД) и фактическое (actual, РД/ИД) значения по
// правилу. Пороговые правила сравнивают только actual с абсолютным нормативом — MISSING_EVIDENCE
// проверяется лишь для того значения, которое реально нужно правилу.
func Evaluate(rule Rule, expectedRaw, actualRaw string) Result {
	switch rule.Type {
	case TypeThresholdMin, TypeThresholdMax:
		if strings.TrimSpace(actualRaw) == "" {
			return missing("нет значения в РД/ИД для проверки порога")
		}
		return evalThreshold(rule, actualRaw)
	case TypeNumericEqual, TypeNumericDeltaPct, TypeNumericDecrease, TypeNumericIncrease,
		TypeOrdinalDecrease, TypeTextMismatch:
		if strings.TrimSpace(expectedRaw) == "" || strings.TrimSpace(actualRaw) == "" {
			return missing("нет значения для сравнения (ПД или РД/ИД)")
		}
		switch rule.Type {
		case TypeOrdinalDecrease:
			return evalOrdinalDecrease(rule, expectedRaw, actualRaw)
		case TypeTextMismatch:
			return evalTextMismatch(expectedRaw, actualRaw)
		default:
			return evalNumericPair(rule, expectedRaw, actualRaw)
		}
	default:
		return Result{
			FindingStatus: StatusNotComparable,
			Rationale:     fmt.Sprintf("неизвестный тип правила %q (RULE_NOT_IMPLEMENTED)", rule.Type),
		}
	}
}

// NotComparable — для param_code вообще нет загруженного правила (backend-plan.md §8.5 п.1).
func NotComparable() Result {
	return Result{FindingStatus: StatusNotComparable, Rationale: "правило для параметра не реализовано (RULE_NOT_IMPLEMENTED)"}
}

func missing(why string) Result { return Result{FindingStatus: StatusMissingEvidence, Rationale: why} }

func notComparable(why string) Result {
	return Result{FindingStatus: StatusNotComparable, Rationale: why}
}

func candidate(expected, actual, delta, why string) Result {
	return Result{FindingStatus: StatusCandidate, ExpectedValue: expected, ActualValue: actual, Delta: delta, Rationale: why}
}

func negative(expected, actual string) Result {
	return Result{FindingStatus: StatusNegativeVerified, ExpectedValue: expected, ActualValue: actual}
}

// ---- числа ----

var numberRe = regexp.MustCompile(`[-+]?\d+(?:[.,]\d+)?`)

// ParseNumber разбирает число из «человеческой» записи: пробелы/nbsp как разделители тысяч,
// запятая как десятичный разделитель, хвост с единицами измерения игнорируется («1 250,5 м²» → 1250.5).
func ParseNumber(s string) (float64, error) {
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == ' ' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	m := numberRe.FindString(compact)
	if m == "" {
		return 0, fmt.Errorf("не удалось разобрать числовое значение %q", s)
	}
	return strconv.ParseFloat(strings.Replace(m, ",", ".", 1), 64)
}

func parsePair(a, b string) (float64, float64, error) {
	af, err := ParseNumber(a)
	if err != nil {
		return 0, 0, err
	}
	bf, err := ParseNumber(b)
	if err != nil {
		return 0, 0, err
	}
	return af, bf, nil
}

func fmtNum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func evalNumericPair(rule Rule, expectedRaw, actualRaw string) Result {
	e, a, err := parsePair(expectedRaw, actualRaw)
	if err != nil {
		return notComparable(err.Error())
	}
	delta := fmtNum(round6(a - e))
	pctOf := func() float64 {
		if e == 0 {
			if a == 0 {
				return 0
			}
			return math.Inf(1)
		}
		return math.Abs(a-e) / math.Abs(e) * 100
	}

	switch rule.Type {
	case TypeNumericEqual:
		if a == e || (rule.ToleranceEqual > 0 && pctOf() <= rule.ToleranceEqual) {
			return negative(expectedRaw, actualRaw)
		}
		return candidate(expectedRaw, actualRaw, delta,
			fmt.Sprintf("значение в РД/ИД (%s) не совпадает с ПД (%s)", actualRaw, expectedRaw))
	case TypeNumericDeltaPct:
		if p := pctOf(); p > rule.Pct {
			return candidate(expectedRaw, actualRaw, delta,
				fmt.Sprintf("расхождение %.3g%% превышает допустимые %.3g%%", p, rule.Pct))
		}
		return negative(expectedRaw, actualRaw)
	case TypeNumericDecrease:
		if a < e {
			return candidate(expectedRaw, actualRaw, delta,
				fmt.Sprintf("значение в РД/ИД (%s) меньше, чем в ПД (%s)", actualRaw, expectedRaw))
		}
		return negative(expectedRaw, actualRaw)
	default: // TypeNumericIncrease
		if a > e && (rule.Pct == 0 || pctOf() > rule.Pct) {
			return candidate(expectedRaw, actualRaw, delta,
				fmt.Sprintf("значение в РД/ИД (%s) больше, чем в ПД (%s)", actualRaw, expectedRaw))
		}
		return negative(expectedRaw, actualRaw)
	}
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

func evalThreshold(rule Rule, actualRaw string) Result {
	actual, err := ParseNumber(actualRaw)
	if err != nil {
		return notComparable(err.Error())
	}
	switch {
	case rule.Type == TypeThresholdMin && actual < rule.Threshold:
		return candidate("", actualRaw, "", fmt.Sprintf("значение %.4g меньше минимально допустимого %.4g", actual, rule.Threshold))
	case rule.Type == TypeThresholdMax && actual > rule.Threshold:
		return candidate("", actualRaw, "", fmt.Sprintf("значение %.4g больше максимально допустимого %.4g", actual, rule.Threshold))
	}
	return negative("", actualRaw)
}

// ---- порядковые шкалы и текст ----

// cyrillicLookalikes — кириллица, неотличимая от латиницы: в шифрах и марках («В30», «С345», «А500С»)
// её пишут вперемешку, для сравнения приводим к латинице.
var cyrillicLookalikes = strings.NewReplacer(
	"А", "A", "В", "B", "С", "C", "Е", "E", "К", "K", "М", "M", "Н", "H", "О", "O", "Р", "P", "Т", "T", "Х", "X",
)

func normalizeToken(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = cyrillicLookalikes.Replace(s)
	return strings.ReplaceAll(s, ",", ".")
}

var tokenSplitRe = regexp.MustCompile(`[\s;/()]+`)

// rankOf ищет ранг значения в шкале: сначала точное совпадение токена с алиасом, затем — самый длинный
// алиас-префикс («A500C» → A500, «B30W6» → B30). Возвращает -1, если значение не из шкалы.
func rankOf(order [][]string, raw string) int {
	norm := normalizeToken(raw)
	tokens := tokenSplitRe.Split(norm, -1)
	for _, t := range tokens {
		for rank, aliases := range order {
			for _, al := range aliases {
				if t == al {
					return rank
				}
			}
		}
	}
	best, bestLen := -1, 0
	for _, t := range tokens {
		for rank, aliases := range order {
			for _, al := range aliases {
				if len(al) >= 2 && len(al) > bestLen && strings.HasPrefix(t, al) {
					best, bestLen = rank, len(al)
				}
			}
		}
	}
	return best
}

func evalOrdinalDecrease(rule Rule, expectedRaw, actualRaw string) Result {
	er, ar := rankOf(rule.scaleOrder, expectedRaw), rankOf(rule.scaleOrder, actualRaw)
	if er < 0 || ar < 0 {
		return notComparable(fmt.Sprintf("значение не найдено в шкале %q: %q / %q", rule.Scale, expectedRaw, actualRaw))
	}
	if ar < er {
		return candidate(expectedRaw, actualRaw, "",
			fmt.Sprintf("понижение по шкале %q: в РД/ИД %q ниже, чем в ПД %q", rule.Scale, actualRaw, expectedRaw))
	}
	return negative(expectedRaw, actualRaw)
}

var nonWordRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

func normalizeText(s string) string {
	return strings.TrimSpace(nonWordRe.ReplaceAllString(strings.ToLower(s), " "))
}

func evalTextMismatch(expectedRaw, actualRaw string) Result {
	if normalizeText(expectedRaw) == normalizeText(actualRaw) {
		return negative(expectedRaw, actualRaw)
	}
	return candidate(expectedRaw, actualRaw, "",
		fmt.Sprintf("формулировка в РД/ИД (%q) отличается от ПД (%q)", actualRaw, expectedRaw))
}

package rules_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine/rules"
)

func TestParseNumber(t *testing.T) {
	for in, want := range map[string]float64{
		"1250,5": 1250.5, "1 250,5 м²": 1250.5, "1 250.5": 1250.5, "0.9": 0.9, "-3": -3, "45 %": 45,
	} {
		got, err := rules.ParseNumber(in)
		require.NoError(t, err, in)
		require.InDelta(t, want, got, 1e-9, in)
	}
	_, err := rules.ParseNumber("нет данных")
	require.Error(t, err)
}

func TestEvaluate_NewTypes(t *testing.T) {
	scales, err := rules.LoadDir("../../../rules")
	require.NoError(t, err)

	cases := []struct {
		name     string
		rule     rules.Rule
		exp, act string
		want     string
	}{
		{"delta_pct в допуске", rules.Rule{Type: rules.TypeNumericDeltaPct, Pct: 1}, "1000", "1005", rules.StatusNegativeVerified},
		{"delta_pct превышен", rules.Rule{Type: rules.TypeNumericDeltaPct, Pct: 1}, "1000", "1020", rules.StatusCandidate},
		{"delta_pct эталон 0", rules.Rule{Type: rules.TypeNumericDeltaPct, Pct: 1}, "0", "5", rules.StatusCandidate},
		{"decrease", rules.Rule{Type: rules.TypeNumericDecrease}, "120", "118", rules.StatusCandidate},
		{"decrease рост допустим", rules.Rule{Type: rules.TypeNumericDecrease}, "120", "130", rules.StatusNegativeVerified},
		{"increase", rules.Rule{Type: rules.TypeNumericIncrease}, "100", "101", rules.StatusCandidate},
		{"increase с порогом в допуске", rules.Rule{Type: rules.TypeNumericIncrease, Pct: 10}, "100", "105", rules.StatusNegativeVerified},
		{"increase с порогом превышен", rules.Rule{Type: rules.TypeNumericIncrease, Pct: 10}, "100", "111", rules.StatusCandidate},
		{"equal с tolerance", rules.Rule{Type: rules.TypeNumericEqual, ToleranceEqual: 1}, "100", "100,5", rules.StatusNegativeVerified},
		{"equal запятая и пробелы", rules.Rule{Type: rules.TypeNumericEqual}, "1 250,5", "1250.5", rules.StatusNegativeVerified},
		{"text одинаковый", rules.Rule{Type: rules.TypeTextMismatch}, "RAL 9003, Сигнальный белый", "ral 9003 сигнальный белый", rules.StatusNegativeVerified},
		{"text разный", rules.Rule{Type: rules.TypeTextMismatch}, "RAL 9003", "RAL 7016", rules.StatusCandidate},
		{"нет ПД", rules.Rule{Type: rules.TypeNumericDecrease}, "", "5", rules.StatusMissingEvidence},
		{"нечисловое", rules.Rule{Type: rules.TypeNumericDecrease}, "abc", "5", rules.StatusNotComparable},
		{"threshold без ПД", rules.Rule{Type: rules.TypeThresholdMin, Threshold: 0.9}, "", "0,8", rules.StatusCandidate},
		{"threshold max граница", rules.Rule{Type: rules.TypeThresholdMax, Threshold: 0.014}, "", "0.014", rules.StatusNegativeVerified},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, rules.Evaluate(c.rule, c.exp, c.act).FindingStatus)
		})
	}

	// Порядковые шкалы — на реальных правилах из каталога, чтобы ловить опечатки в scales.yaml.
	ordinal := []struct {
		code     string
		exp, act string
		want     string
	}{
		{"M-055", "B35", "B30", rules.StatusCandidate},
		{"M-055", "В30 W6 F150", "B30", rules.StatusNegativeVerified}, // кириллическая В + хвост марки
		{"M-055", "B30", "B35", rules.StatusNegativeVerified},         // повышение — не нарушение
		{"M-055", "B7,5", "B10", rules.StatusNegativeVerified},
		{"M-057", "А500С", "А400", rules.StatusCandidate},
		{"M-057", "A500C", "А500С", rules.StatusNegativeVerified},
		{"M-056", "С345", "С245", rules.StatusCandidate},
		{"M-022", "I степень", "II", rules.StatusCandidate},
		{"M-022", "II", "I", rules.StatusNegativeVerified},
		{"M-022", "III", "II", rules.StatusNegativeVerified},
		{"M-023", "С0", "С1", rules.StatusCandidate},
		{"M-021", "A+", "B", rules.StatusCandidate},
		{"M-021", "A++", "A+", rules.StatusCandidate},
		{"M-124", "B", "A", rules.StatusNegativeVerified},
		{"M-015", "1 категория", "2", rules.StatusCandidate},
		{"M-107", "КМ0", "КМ3", rules.StatusCandidate},
		{"M-107", "КМ3", "КМ1", rules.StatusNegativeVerified},
		{"M-055", "B30", "неизвестно", rules.StatusNotComparable},
	}
	for _, c := range ordinal {
		t.Run(c.code+"/"+c.exp+"->"+c.act, func(t *testing.T) {
			rule, ok := scales[c.code]
			require.True(t, ok, "нет правила %s", c.code)
			require.Equal(t, c.want, rules.Evaluate(rule, c.exp, c.act).FindingStatus)
		})
	}
}

// Каталог правил — часть продукта: проверяем его целиком, а не только механизм.
func TestCatalog_LoadsAndIsWellFormed(t *testing.T) {
	set, err := rules.LoadDir("../../../rules")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(set), 50, "ожидался каталог из десятков правил Матрицы")

	codeRe := regexp.MustCompile(`^M-(\d{3})$`)
	for code, r := range set {
		require.Equal(t, code, r.Code)
		m := codeRe.FindStringSubmatch(code)
		require.NotNil(t, m, "код %q не из Матрицы M-001…M-132", code)
		require.LessOrEqual(t, m[1], "132", code)
		require.NotEmpty(t, r.Description, code)
		switch r.Type {
		case rules.TypeThresholdMin, rules.TypeThresholdMax:
			require.NotZero(t, r.Threshold, code)
		case rules.TypeNumericDeltaPct:
			require.NotZero(t, r.Pct, code)
		case rules.TypeOrdinalDecrease, rules.TypeNumericEqual, rules.TypeNumericDecrease,
			rules.TypeNumericIncrease, rules.TypeTextMismatch:
		default:
			t.Fatalf("правило %s: неизвестный тип %q", code, r.Type)
		}
	}
}

func TestLoadDir_UnknownScaleFails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "M-999.yaml", "code: M-999\ntype: ordinal_decrease\nscale: nope\n")
	_, err := rules.LoadDir(dir)
	require.ErrorContains(t, err, "неизвестная шкала")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

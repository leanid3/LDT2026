package rules_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine/rules"
)

func TestEvaluate_NumericEqual_Match(t *testing.T) {
	r := rules.Rule{Type: rules.TypeNumericEqual}
	res := rules.Evaluate(r, "100", "100")
	require.Equal(t, rules.StatusNegativeVerified, res.FindingStatus)
}

func TestEvaluate_NumericEqual_Mismatch(t *testing.T) {
	r := rules.Rule{Type: rules.TypeNumericEqual}
	res := rules.Evaluate(r, "100", "90")
	require.Equal(t, rules.StatusCandidate, res.FindingStatus)
	require.Equal(t, "100", res.ExpectedValue)
	require.Equal(t, "90", res.ActualValue)
	require.Equal(t, "-10", res.Delta)
}

func TestEvaluate_ThresholdMin(t *testing.T) {
	r := rules.Rule{Type: rules.TypeThresholdMin, Threshold: 0.9}
	require.Equal(t, rules.StatusCandidate, rules.Evaluate(r, "1", "0.8").FindingStatus)
	require.Equal(t, rules.StatusNegativeVerified, rules.Evaluate(r, "1", "0.95").FindingStatus)
	require.Equal(t, rules.StatusNegativeVerified, rules.Evaluate(r, "1", "0.9").FindingStatus, "равно порогу — не нарушение")
}

func TestEvaluate_ThresholdMax(t *testing.T) {
	r := rules.Rule{Type: rules.TypeThresholdMax, Threshold: 4.2}
	require.Equal(t, rules.StatusCandidate, rules.Evaluate(r, "1", "5.0").FindingStatus)
	require.Equal(t, rules.StatusNegativeVerified, rules.Evaluate(r, "1", "4.0").FindingStatus)
}

func TestEvaluate_MissingEvidence(t *testing.T) {
	r := rules.Rule{Type: rules.TypeNumericEqual}
	require.Equal(t, rules.StatusMissingEvidence, rules.Evaluate(r, "", "1").FindingStatus)
	require.Equal(t, rules.StatusMissingEvidence, rules.Evaluate(r, "1", "").FindingStatus)
}

func TestEvaluate_UnparsableValue_NotComparable(t *testing.T) {
	r := rules.Rule{Type: rules.TypeNumericEqual}
	require.Equal(t, rules.StatusNotComparable, rules.Evaluate(r, "abc", "1").FindingStatus)
}

func TestEvaluate_UnknownRuleType(t *testing.T) {
	r := rules.Rule{Type: "some_future_type"}
	require.Equal(t, rules.StatusNotComparable, rules.Evaluate(r, "1", "1").FindingStatus)
}

func TestNotComparable(t *testing.T) {
	require.Equal(t, rules.StatusNotComparable, rules.NotComparable().FindingStatus)
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "demo-001.yaml"), []byte("code: DEMO-001\ntype: numeric_equal\ndescription: test\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "demo-002.yaml"), []byte("code: DEMO-002\ntype: threshold_min\nthreshold: 0.9\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0o644))

	loaded, err := rules.LoadDir(dir)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	require.Equal(t, rules.TypeNumericEqual, loaded["DEMO-001"].Type)
	require.Equal(t, 0.9, loaded["DEMO-002"].Threshold)
}

func TestLoadDir_MissingCode(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("type: numeric_equal\n"), 0o644))

	_, err := rules.LoadDir(dir)
	require.Error(t, err)
}

package findings_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/findings"
)

func ptr[T any](v T) *T { return &v }

func TestToAPI_MapsCardAndEvidence(t *testing.T) {
	file := uuid.New()
	f := findings.Finding{
		ID: uuid.New(), CheckID: uuid.New(), FindingStatus: "CANDIDATE", InspectorStatus: "PENDING", Version: 1,
		DecidedAt: ptr(time.Now()),
		ParamCode: ptr("M-055"), ParameterName: ptr("Класс бетона"), Unit: ptr("Марка (B)"), ReviewPriority: ptr("HIGH"),
		ExpectedValue: ptr("B35"), ActualValue: ptr("B30"), Delta: ptr("-1"), Rationale: ptr("понижение"),
		Evidence: []findings.Evidence{
			{FileID: file, OriginalName: "rd.pdf", Stage: ptr("RD"), Page: ptr(7), BBox: []float64{0.1, 0.2, 0.6, 0.25},
				Quote: ptr("В30"), ExtractedValue: ptr("B30"), Role: "actual"},
			{FileID: file, Role: "expected"}, // без имени/стадии/геометрии — поля просто отсутствуют
		},
	}
	got := findings.ToAPI(f)

	require.Equal(t, "M-055", *got.ParamCode)
	require.Equal(t, "Класс бетона", *got.ParameterName)
	require.Equal(t, "HIGH", string(*got.ReviewPriority))
	require.Equal(t, "B35", *got.ExpectedValue)
	require.Equal(t, "B30", *got.ActualValue)
	require.Equal(t, "понижение", *got.Rationale)

	require.NotNil(t, got.Evidence)
	ev := *got.Evidence
	require.Len(t, ev, 2)
	require.Equal(t, "rd.pdf", *ev[0].OriginalName)
	require.Equal(t, "RD", string(*ev[0].Stage))
	require.Equal(t, 7, *ev[0].Page)
	require.Equal(t, []float32{0.1, 0.2, 0.6, 0.25}, *ev[0].Bbox)
	require.Equal(t, "actual", string(ev[0].Role))
	require.Nil(t, ev[1].Bbox)
	require.Nil(t, ev[1].OriginalName)
	require.Nil(t, ev[1].Stage)
}

func TestToAPI_NoCardOmitsOptionalFields(t *testing.T) {
	got := findings.ToAPI(findings.Finding{ID: uuid.New(), CheckID: uuid.New(), FindingStatus: "NOT_COMPARABLE", InspectorStatus: "PENDING"})
	require.Nil(t, got.ParamCode)
	require.Nil(t, got.Evidence, "пустые доказательства не превращаются в []")
	require.Nil(t, got.ReviewPriority)
}

package process

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComputeScenario(t *testing.T) {
	cases := []struct {
		name                      string
		hasPD, hasRD, hasID, part bool
		want                      string
	}{
		{"none", false, false, false, false, ""},
		{"full", true, true, true, false, ScenarioFull},
		{"pd_rd", true, true, false, false, ScenarioPDRDOnly},
		{"pd_id", true, false, true, false, ScenarioPDIDOnly},
		{"rd_id", false, true, true, false, ScenarioRDIDOnly},
		{"single_pd", true, false, false, false, ScenarioSingleOnly},
		{"single_rd", false, true, false, false, ScenarioSingleOnly},
		{"single_id", false, false, true, false, ScenarioSingleOnly},
		{"partial_overrides_full", true, true, true, true, ScenarioPartiallyLoaded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeScenario(tc.hasPD, tc.hasRD, tc.hasID, tc.part)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPlanParseJobs_PDF_MultiplePages(t *testing.T) {
	jobs := planParseJobs(CurrentFile{IsPDF: true, PageCount: 45})
	require.Len(t, jobs, 3)

	require.Equal(t, 1, *jobs[0].From)
	require.Equal(t, 20, *jobs[0].To)
	require.Equal(t, 21, *jobs[1].From)
	require.Equal(t, 40, *jobs[1].To)
	require.Equal(t, 41, *jobs[2].From)
	require.Equal(t, 45, *jobs[2].To)
}

func TestPlanParseJobs_PDF_ExactMultiple(t *testing.T) {
	jobs := planParseJobs(CurrentFile{IsPDF: true, PageCount: 40})
	require.Len(t, jobs, 2)
	require.Equal(t, 40, *jobs[1].To)
}

func TestPlanParseJobs_PDF_SinglePage(t *testing.T) {
	jobs := planParseJobs(CurrentFile{IsPDF: true, PageCount: 1})
	require.Len(t, jobs, 1)
	require.Equal(t, 1, *jobs[0].From)
	require.Equal(t, 1, *jobs[0].To)
}

func TestPlanParseJobs_NonPDF_SingleJob(t *testing.T) {
	jobs := planParseJobs(CurrentFile{IsPDF: false})
	require.Len(t, jobs, 1)
	require.Nil(t, jobs[0].From)
	require.Nil(t, jobs[0].To)
}

func TestPlanParseJobs_PDF_UnknownPageCount(t *testing.T) {
	jobs := planParseJobs(CurrentFile{IsPDF: true, PageCount: 0})
	require.Len(t, jobs, 1, "без известного числа страниц — одно задание, как для не-PDF")
	require.Nil(t, jobs[0].From)
}

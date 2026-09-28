package files_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/files"
)

const csvSample = `object_id,file_name,sha256,doc_stage,discipline,document_code,revision,approval_status,approval_date,sheet_page_range,predecessor_ref,successor_ref,signature_status
obj-1,pd-001.pdf,aaaa,PD,АР,АР-001,1,APPROVED,2026-01-01,1-10,,bbbb,SIGNED
obj-1,pd-001-v2.pdf,bbbb,PD,АР,АР-001,2,FOR_CONSTRUCTION,2026-02-01,1-12,aaaa,,SIGNED
`

func TestParseRegistryCSV(t *testing.T) {
	rows, err := files.ParseRegistryCSV(strings.NewReader(csvSample))
	require.NoError(t, err)
	require.Len(t, rows, 2)

	require.Equal(t, "obj-1", rows[0].ObjectID)
	require.Equal(t, "pd-001.pdf", rows[0].FileName)
	require.Equal(t, "aaaa", rows[0].SHA256)
	require.Equal(t, "PD", rows[0].DocStage)
	require.Equal(t, "APPROVED", rows[0].ApprovalStatus)
	require.Equal(t, "bbbb", rows[0].SuccessorRef)
	require.Empty(t, rows[0].PredecessorRef)

	require.Equal(t, "aaaa", rows[1].PredecessorRef)
}

const jsonSample = `[
	{"object_id":"obj-1","file_name":"pd-001.pdf","sha256":"aaaa","doc_stage":"PD","approval_status":"APPROVED"},
	{"object_id":"obj-1","file_name":"pd-001-v2.pdf","sha256":"bbbb","doc_stage":"PD","approval_status":"FOR_CONSTRUCTION","predecessor_ref":"aaaa"}
]`

func TestParseRegistryJSON(t *testing.T) {
	rows, err := files.ParseRegistryJSON(strings.NewReader(jsonSample))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "aaaa", rows[0].SHA256)
	require.Equal(t, "aaaa", rows[1].PredecessorRef)
}

func TestParseRegistryXLSX(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := f.GetSheetName(0)

	header := []string{"object_id", "file_name", "sha256", "doc_stage", "discipline", "document_code",
		"revision", "approval_status", "approval_date", "sheet_page_range", "predecessor_ref", "successor_ref", "signature_status"}
	for i, h := range header {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		require.NoError(t, f.SetCellValue(sheet, cell, h))
	}
	row := []string{"obj-1", "pd-001.pdf", "aaaa", "PD", "АР", "АР-001", "1", "APPROVED", "2026-01-01", "1-10", "", "", "SIGNED"}
	for i, v := range row {
		cell, _ := excelize.CoordinatesToCellName(i+1, 2)
		require.NoError(t, f.SetCellValue(sheet, cell, v))
	}

	buf, err := f.WriteToBuffer()
	require.NoError(t, err)

	rows, err := files.ParseRegistryXLSX(buf)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "obj-1", rows[0].ObjectID)
	require.Equal(t, "aaaa", rows[0].SHA256)
	require.Equal(t, "APPROVED", rows[0].ApprovalStatus)
}

func TestParseRegistry_DispatchByExtension(t *testing.T) {
	rows, err := files.ParseRegistry("registry.csv", strings.NewReader(csvSample))
	require.NoError(t, err)
	require.Len(t, rows, 2)

	_, err = files.ParseRegistry("registry.txt", strings.NewReader(""))
	require.Error(t, err)
}

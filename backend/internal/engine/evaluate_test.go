package engine_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine/rules"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
)

func TestProcessFacts_CreatesEvidenceGroupsChecksFindings(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	objectID := uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'Объект')`, objectID)
	require.NoError(t, err)
	processID := uuid.New()
	_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'PARSING')`, processID, objectID)
	require.NoError(t, err)

	fileID := uuid.New()
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO files (id, object_id, process_id, original_name, storage_key, size_bytes, check_status, is_current)
		VALUES ($1, $2, $3, 'a.pdf', 'k', 1, 'ACCEPTED', true)
	`, fileID, objectID, processID)
	require.NoError(t, err)

	ruleSet := map[string]rules.Rule{
		"DEMO-001": {Code: "DEMO-001", Type: rules.TypeNumericEqual},
		"DEMO-002": {Code: "DEMO-002", Type: rules.TypeThresholdMin, Threshold: 0.9},
	}

	facts := []engine.Fact{
		{FactID: "f1", FileID: fileID.String(), Stage: "PD", ParamCode: "DEMO-001", ValueRaw: "100", Quote: "площадь 100"},
		{FactID: "f2", FileID: fileID.String(), Stage: "RD", ParamCode: "DEMO-001", ValueRaw: "90", Quote: "площадь 90"},
		{FactID: "f3", FileID: fileID.String(), Stage: "RD", ParamCode: "DEMO-002", ValueRaw: "1.2", Quote: "ширина 1.2"},
		{FactID: "f4", FileID: fileID.String(), Stage: "PD", ParamCode: "UNKNOWN-CODE", ValueRaw: "x", Quote: "неизвестный параметр"},
	}

	err = database.WithTx(ctx, func(tx pgx.Tx) error {
		return engine.ProcessFacts(ctx, tx, processID, objectID, facts, ruleSet)
	})
	require.NoError(t, err)

	rows, err := database.Pool().Query(ctx, `
		SELECT eg.param_code, c.finding_status, f.inspector_status
		FROM evidence_groups eg
		JOIN checks c ON c.evidence_group_id = eg.id
		JOIN findings f ON f.check_id = c.id
		WHERE eg.process_id = $1
		ORDER BY eg.param_code
	`, processID)
	require.NoError(t, err)
	defer rows.Close()

	type row struct{ paramCode, findingStatus, inspectorStatus string }
	var got []row
	for rows.Next() {
		var r row
		require.NoError(t, rows.Scan(&r.paramCode, &r.findingStatus, &r.inspectorStatus))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())

	require.Len(t, got, 3)
	require.Equal(t, row{"DEMO-001", "CANDIDATE", "PENDING"}, got[0], "100 != 90 -> CANDIDATE")
	require.Equal(t, row{"DEMO-002", "NEGATIVE_VERIFIED", "PENDING"}, got[1], "1.2 >= 0.9 -> NEGATIVE_VERIFIED")
	require.Equal(t, row{"UNKNOWN-CODE", "NOT_COMPARABLE", "PENDING"}, got[2], "нет правила -> NOT_COMPARABLE")

	var fragmentCount int
	require.NoError(t, database.Pool().QueryRow(ctx, `
		SELECT count(*) FROM evidence_fragments ef
		JOIN evidence_groups eg ON eg.id = ef.evidence_group_id
		WHERE eg.process_id = $1 AND eg.param_code = 'DEMO-001'
	`, processID).Scan(&fragmentCount))
	require.Equal(t, 2, fragmentCount, "expected + actual фрагменты для DEMO-001")
}

func TestProcessFacts_MissingEvidence(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	objectID := uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'Объект')`, objectID)
	require.NoError(t, err)
	processID := uuid.New()
	_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'PARSING')`, processID, objectID)
	require.NoError(t, err)
	fileID := uuid.New()
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO files (id, object_id, process_id, original_name, storage_key, size_bytes, check_status, is_current)
		VALUES ($1, $2, $3, 'a.pdf', 'k', 1, 'ACCEPTED', true)
	`, fileID, objectID, processID)
	require.NoError(t, err)

	ruleSet := map[string]rules.Rule{"DEMO-001": {Code: "DEMO-001", Type: rules.TypeNumericEqual}}
	facts := []engine.Fact{
		{FactID: "f1", FileID: fileID.String(), Stage: "PD", ParamCode: "DEMO-001", ValueRaw: "100"},
		// нет RD/ID факта для DEMO-001
	}

	err = database.WithTx(ctx, func(tx pgx.Tx) error {
		return engine.ProcessFacts(ctx, tx, processID, objectID, facts, ruleSet)
	})
	require.NoError(t, err)

	var status string
	require.NoError(t, database.Pool().QueryRow(ctx, `
		SELECT finding_status FROM checks WHERE process_id = $1
	`, processID).Scan(&status))
	require.Equal(t, "MISSING_EVIDENCE", status)
}

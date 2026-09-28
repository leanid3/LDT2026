package matrix_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine/rules"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/matrix"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
)

const xlsx = "../../../docs/source/Матрица_параметров_редакция1.1.xlsx"

func TestReadXLSX_RealMatrix(t *testing.T) {
	params, err := matrix.ReadXLSX(xlsx)
	require.NoError(t, err)
	require.Len(t, params, 132, "ТЗ: Матрица содержит 132 параметра")
	require.Equal(t, "M-001", params[0].Code)
	require.Equal(t, "M-132", params[131].Code)

	first := params[0]
	require.Equal(t, "Площадь застройки", first.Name)
	require.Equal(t, "м²", first.Unit)
	require.Equal(t, "HIGH", first.ReviewPriority)
	require.NotEmpty(t, first.SourcePD)
	require.NotEmpty(t, first.TriggerLogic)

	for _, p := range params {
		require.NotEmpty(t, p.Name, p.Code)
		require.Contains(t, []string{"HIGH", "MEDIUM", "LOW"}, p.ReviewPriority, p.Code)
	}
}

func TestEnrich_RuleCatalogCoversRealCodes(t *testing.T) {
	params, err := matrix.ReadXLSX(xlsx)
	require.NoError(t, err)
	set, err := rules.LoadDir("../../rules")
	require.NoError(t, err)

	known := map[string]bool{}
	for _, p := range params {
		known[p.Code] = true
	}
	for code := range set {
		require.True(t, known[code], "правило %s есть, а параметра в Матрице нет", code)
	}

	matrix.Enrich(params, set)
	byCode := map[string]matrix.Param{}
	for _, p := range params {
		byCode[p.Code] = p
	}
	require.Equal(t, "ordinal", byCode["M-055"].DataType)
	require.Equal(t, "concrete", byCode["M-055"].Scale)
	require.Equal(t, "number", byCode["M-041"].DataType)
	require.Equal(t, "numeric_equal", byCode["M-001"].RuleType)
	require.Equal(t, "string", byCode["M-011"].DataType, "правила нет — параметр только извлекается")
}

func TestUpsert_IdempotentAndDeactivates(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	params, err := matrix.ReadXLSX(xlsx)
	require.NoError(t, err)
	require.NoError(t, matrix.Upsert(ctx, database, params, "1.1"))
	require.NoError(t, matrix.Upsert(ctx, database, params, "1.1"), "повторный импорт не должен падать")

	var total, active int
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE is_active) FROM params`).Scan(&total, &active))
	require.Equal(t, 132, total)
	require.Equal(t, 132, active)

	// Новая редакция без последних 2 параметров: они деактивируются, а не удаляются.
	require.NoError(t, matrix.Upsert(ctx, database, params[:130], "1.2"))
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE is_active) FROM params`).Scan(&total, &active))
	require.Equal(t, 132, total)
	require.Equal(t, 130, active)

	var version string
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT matrix_version FROM params WHERE code = 'M-001'`).Scan(&version))
	require.Equal(t, "1.2", version)
}

func TestRepository_ListActive(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()
	params, err := matrix.ReadXLSX(xlsx)
	require.NoError(t, err)
	require.NoError(t, matrix.Upsert(ctx, database, params, "1.1"))
	require.NoError(t, matrix.Upsert(ctx, database, params[:100], "1.2")) // 32 параметра исчезли из редакции

	version, got, err := matrix.NewRepository(database.Pool()).ListActive(ctx)
	require.NoError(t, err)
	require.Equal(t, "1.2", version)
	require.Len(t, got, 100, "деактивированные параметры в каталог интерфейса не попадают")
	require.Equal(t, "M-001", got[0].Code)
	require.Equal(t, "Площадь застройки", got[0].Name)
	require.Equal(t, "м²", got[0].Unit)
	require.Equal(t, "HIGH", got[0].ReviewPriority)
}

package findings_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/findings"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/process"
)

// Карточка finding для интерфейса: параметр Матрицы, значения, пояснение и доказательства обеих
// сторон с bbox — без них инспектору нечего показать (docs: «Локализация доказательств»).
func TestGetDetailed_CardWithEvidence(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()
	fx := setupFixture(t, database, process.StatusReady)

	_, err := database.Pool().Exec(ctx, `
		INSERT INTO params (code, section, parameter_name, unit, review_priority, matrix_version)
		VALUES ('M-055', 'Раздел 4. КР', 'Класс прочности бетона', 'Марка (B)', 'HIGH', '1.1')`)
	require.NoError(t, err)

	var objectID, groupID, checkID uuid.UUID
	require.NoError(t, database.Pool().QueryRow(ctx, `
		SELECT p.object_id, eg.id, c.id FROM processes p
		JOIN evidence_groups eg ON eg.process_id = p.id JOIN checks c ON c.evidence_group_id = eg.id
		WHERE p.id = $1`, fx.processID).Scan(&objectID, &groupID, &checkID))

	_, err = database.Pool().Exec(ctx, `UPDATE evidence_groups SET param_code = 'M-055' WHERE id = $1`, groupID)
	require.NoError(t, err)
	_, err = database.Pool().Exec(ctx, `
		UPDATE checks SET expected_value = 'B35', actual_value = 'B30', delta = '-1', rationale = 'понижение по шкале' WHERE id = $1`, checkID)
	require.NoError(t, err)

	fileID := uuid.New()
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO files (id, object_id, process_id, original_name, storage_key, size_bytes, check_status)
		VALUES ($1, $2, $3, 'pd.pdf', 'k', 1, 'ACCEPTED')`, fileID, objectID, fx.processID)
	require.NoError(t, err)
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO evidence_fragments (evidence_group_id, file_id, stage, sheet_page, bbox_polygon_norm, extracted_value, quote, role_expected_actual)
		VALUES ($1, $2, 'RD', 7, '[0.1,0.2,0.6,0.25]', 'B30', 'Бетон класса В30', 'actual'),
		       ($1, $2, 'PD', 3, NULL,                 'B35', 'Бетон класса В35', 'expected')`, groupID, fileID)
	require.NoError(t, err)

	f, err := findings.NewService(database).Get(ctx, fx.findingID)
	require.NoError(t, err)

	require.Equal(t, "M-055", *f.ParamCode)
	require.Equal(t, "Класс прочности бетона", *f.ParameterName)
	require.Equal(t, "Марка (B)", *f.Unit)
	require.Equal(t, "HIGH", *f.ReviewPriority)
	require.Equal(t, "B35", *f.ExpectedValue)
	require.Equal(t, "B30", *f.ActualValue)
	require.Equal(t, "понижение по шкале", *f.Rationale)

	require.Len(t, f.Evidence, 2)
	require.Equal(t, "actual", f.Evidence[0].Role, "порядок стабилен: role по алфавиту")
	require.Equal(t, []float64{0.1, 0.2, 0.6, 0.25}, f.Evidence[0].BBox)
	require.Equal(t, 7, *f.Evidence[0].Page)
	require.Equal(t, "pd.pdf", f.Evidence[0].OriginalName)
	require.Equal(t, "Бетон класса В30", *f.Evidence[0].Quote)
	require.Equal(t, "expected", f.Evidence[1].Role)
	require.Nil(t, f.Evidence[1].BBox, "геометрии нет — nil, а не нулевой прямоугольник")
}

func TestGetDetailed_FindingWithoutParamStillLoads(t *testing.T) {
	database := dbtest.NewPostgres(t)
	fx := setupFixture(t, database, process.StatusReady) // группа без param_code, без фрагментов
	f, err := findings.NewService(database).Get(context.Background(), fx.findingID)
	require.NoError(t, err)
	require.Nil(t, f.ParamCode)
	require.Empty(t, f.Evidence)
}

func TestDecide_ReturnsDetailedCard(t *testing.T) {
	database := dbtest.NewPostgres(t)
	fx := setupFixture(t, database, process.StatusReady)
	actorID := createActor(t, database)
	comment := "подтверждаю"
	f, err := findings.NewService(database).Decide(context.Background(), fx.findingID, actorID,
		findings.Decision{Decision: findings.InspectorConfirmedViolation, Comment: &comment})
	require.NoError(t, err)
	require.Equal(t, findings.InspectorConfirmedViolation, f.InspectorStatus)
	require.NotNil(t, f.DecidedAt)
}

// Порядок для работы инспектора: то, что требует решения, — первым; внутри — по приоритету параметра.
func TestListByProcess_OrderedForInspector(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()
	fx := setupFixture(t, database, process.StatusReady)

	var objectID uuid.UUID
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT object_id FROM processes WHERE id = $1`, fx.processID).Scan(&objectID))
	for _, p := range [][3]string{{"M-100", "Низкий", "LOW"}, {"M-200", "Высокий", "HIGH"}, {"M-300", "Средний", "MEDIUM"}} {
		_, err := database.Pool().Exec(ctx, `INSERT INTO params (code, parameter_name, review_priority, matrix_version) VALUES ($1, $2, $3, '1.1')`, p[0], p[1], p[2])
		require.NoError(t, err)
	}
	add := func(code, status string) {
		g, c := uuid.New(), uuid.New()
		_, err := database.Pool().Exec(ctx, `INSERT INTO evidence_groups (id, process_id, object_id, param_code, input_hash) VALUES ($1,$2,$3,$4,'h')`, g, fx.processID, objectID, code)
		require.NoError(t, err)
		_, err = database.Pool().Exec(ctx, `INSERT INTO checks (id, process_id, object_id, evidence_group_id, finding_status) VALUES ($1,$2,$3,$4,$5)`, c, fx.processID, objectID, g, status)
		require.NoError(t, err)
		_, err = database.Pool().Exec(ctx, `INSERT INTO findings (check_id, finding_status, inspector_status) VALUES ($1,$2,'PENDING')`, c, status)
		require.NoError(t, err)
	}
	add("M-100", "CANDIDATE")
	add("M-200", "NEGATIVE_VERIFIED")
	add("M-300", "CANDIDATE")
	add("M-200", "CANDIDATE")

	got, err := findings.NewRepository(database.Pool()).ListByProcess(ctx, fx.processID)
	require.NoError(t, err)

	var order []string
	for _, f := range got {
		code := "-"
		if f.ParamCode != nil {
			code = *f.ParamCode
		}
		order = append(order, f.FindingStatus+":"+code)
	}
	// fixture-finding (без параметра) — тоже CANDIDATE, приоритета у него нет -> в конец группы CANDIDATE.
	require.Equal(t, []string{
		"CANDIDATE:M-200", "CANDIDATE:M-300", "CANDIDATE:M-100", "CANDIDATE:-", "NEGATIVE_VERIFIED:M-200",
	}, order)
}

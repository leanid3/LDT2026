package findings_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/findings"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/process"
)

// fixture — объект + процесс (заданный статус) + один check с одним CANDIDATE finding.
type fixture struct {
	processID uuid.UUID
	findingID uuid.UUID
}

func setupFixture(t *testing.T, database *db.DB, processStatus string) fixture {
	t.Helper()
	ctx := context.Background()

	objectID := uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'Объект')`, objectID)
	require.NoError(t, err)

	processID := uuid.New()
	_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, $3)`, processID, objectID, processStatus)
	require.NoError(t, err)

	evidenceGroupID := uuid.New()
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO evidence_groups (id, process_id, object_id, input_hash) VALUES ($1, $2, $3, 'hash1')
	`, evidenceGroupID, processID, objectID)
	require.NoError(t, err)

	checkID := uuid.New()
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO checks (id, process_id, object_id, evidence_group_id, finding_status)
		VALUES ($1, $2, $3, $4, 'CANDIDATE')
	`, checkID, processID, objectID, evidenceGroupID)
	require.NoError(t, err)

	findingID := uuid.New()
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO findings (id, check_id, finding_status, inspector_status) VALUES ($1, $2, 'CANDIDATE', 'PENDING')
	`, findingID, checkID)
	require.NoError(t, err)

	return fixture{processID: processID, findingID: findingID}
}

func createActor(t *testing.T, database *db.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := database.Pool().Exec(context.Background(), `
		INSERT INTO users (id, login, password_hash, full_name, role) VALUES ($1, $2, 'x', 'Инспектор', 'inspector')
	`, id, id.String())
	require.NoError(t, err)
	return id
}

func TestDecide_ValidationErrors(t *testing.T) {
	database := dbtest.NewPostgres(t)
	fx := setupFixture(t, database, process.StatusReady)
	actorID := createActor(t, database)
	svc := findings.NewService(database)
	ctx := context.Background()

	_, err := svc.Decide(ctx, fx.findingID, actorID, findings.Decision{Decision: findings.InspectorConfirmedViolation})
	require.ErrorIs(t, err, findings.ErrValidation, "CONFIRMED_VIOLATION без comment")

	_, err = svc.Decide(ctx, fx.findingID, actorID, findings.Decision{Decision: findings.InspectorNegativeVerified, Comment: strPtr("ok")})
	require.ErrorIs(t, err, findings.ErrValidation, "NEGATIVE_VERIFIED без reason_code")

	_, err = svc.Decide(ctx, fx.findingID, actorID, findings.Decision{Decision: "BOGUS"})
	require.ErrorIs(t, err, findings.ErrValidation)
}

func TestDecide_SingleCandidate_ReadyGoesStraightToCompleted(t *testing.T) {
	database := dbtest.NewPostgres(t)
	fx := setupFixture(t, database, process.StatusReady)
	actorID := createActor(t, database)
	svc := findings.NewService(database)
	ctx := context.Background()

	f, err := svc.Decide(ctx, fx.findingID, actorID, findings.Decision{
		Decision: findings.InspectorConfirmedViolation, Comment: strPtr("подтверждено по чертежу"),
	})
	require.NoError(t, err)
	require.Equal(t, findings.InspectorConfirmedViolation, f.InspectorStatus)
	require.Equal(t, 2, f.Version)
	require.NotNil(t, f.DecidedAt)
	require.Equal(t, actorID, *f.DecidedBy)

	var status string
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT status FROM processes WHERE id = $1`, fx.processID).Scan(&status))
	require.Equal(t, process.StatusCompleted, status, "единственный CANDIDATE решён -> COMPLETED в этом же вызове")

	var auditCount int
	require.NoError(t, database.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE object_id = $1 AND action = 'finding.decide'`, fx.findingID.String(),
	).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

func TestDecide_TwoCandidates_StaysVerifyingUntilBothDecided(t *testing.T) {
	database := dbtest.NewPostgres(t)
	fx := setupFixture(t, database, process.StatusReady)
	actorID := createActor(t, database)
	svc := findings.NewService(database)
	ctx := context.Background()

	// второй CANDIDATE на том же процессе
	evidenceGroupID2 := uuid.New()
	_, err := database.Pool().Exec(ctx, `
		INSERT INTO evidence_groups (id, process_id, object_id, input_hash)
		SELECT $1, process_id, object_id, 'hash2' FROM checks WHERE process_id = $2 LIMIT 1
	`, evidenceGroupID2, fx.processID)
	require.NoError(t, err)
	checkID2 := uuid.New()
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO checks (id, process_id, object_id, evidence_group_id, finding_status)
		SELECT $1, process_id, object_id, $2, 'CANDIDATE' FROM checks WHERE process_id = $3 LIMIT 1
	`, checkID2, evidenceGroupID2, fx.processID)
	require.NoError(t, err)
	findingID2 := uuid.New()
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO findings (id, check_id, finding_status, inspector_status) VALUES ($1, $2, 'CANDIDATE', 'PENDING')
	`, findingID2, checkID2)
	require.NoError(t, err)

	_, err = svc.Decide(ctx, fx.findingID, actorID, findings.Decision{
		Decision: findings.InspectorNegativeVerified, ReasonCode: strPtr("OCR_ERROR"), Comment: strPtr("ошибка распознавания"),
	})
	require.NoError(t, err)

	var status string
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT status FROM processes WHERE id = $1`, fx.processID).Scan(&status))
	require.Equal(t, process.StatusVerifying, status, "остался нерешённый CANDIDATE")

	_, err = svc.Decide(ctx, findingID2, actorID, findings.Decision{
		Decision: findings.InspectorClarificationRequired,
	})
	require.NoError(t, err)

	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT status FROM processes WHERE id = $1`, fx.processID).Scan(&status))
	require.Equal(t, process.StatusCompleted, status)
}

func TestDecide_WrongProcessStatus(t *testing.T) {
	database := dbtest.NewPostgres(t)
	fx := setupFixture(t, database, process.StatusPending)
	actorID := createActor(t, database)
	svc := findings.NewService(database)

	_, err := svc.Decide(context.Background(), fx.findingID, actorID, findings.Decision{
		Decision: findings.InspectorClarificationRequired,
	})
	require.ErrorIs(t, err, findings.ErrInvalidTransition)
}

func TestHasPendingCandidates(t *testing.T) {
	database := dbtest.NewPostgres(t)
	fx := setupFixture(t, database, process.StatusReady)
	svc := findings.NewService(database)
	ctx := context.Background()

	err := database.WithTx(ctx, func(tx pgx.Tx) error {
		pending, err := svc.HasPendingCandidates(ctx, tx, fx.processID)
		require.NoError(t, err)
		require.True(t, pending)
		return nil
	})
	require.NoError(t, err)
}

func strPtr(s string) *string { return &s }

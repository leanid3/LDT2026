package process_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/process"
)

type fakeFileLister struct {
	files []process.CurrentFile
	err   error
}

func (f fakeFileLister) ListCurrentAccepted(context.Context, uuid.UUID) ([]process.CurrentFile, error) {
	return f.files, f.err
}

type fakeFindingsGate struct{ pending bool }

func (f fakeFindingsGate) HasPendingCandidates(context.Context, pgx.Tx, uuid.UUID) (bool, error) {
	return f.pending, nil
}

func createTestObjectAndProcess(t *testing.T, database *db.DB) (objectID, processID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	objectID = uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'Объект теста')`, objectID)
	require.NoError(t, err)

	repo := process.NewRepository(database.Pool())
	p, err := repo.Create(ctx, process.Process{ID: uuid.New(), ObjectID: objectID, Status: process.StatusPending})
	require.NoError(t, err)
	return objectID, p.ID
}

// createTestUser — actorID во всех переходах ссылается на users.id (FK finalized_by/audit_log.user_id).
func createTestUser(t *testing.T, database *db.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := database.Pool().Exec(context.Background(), `
		INSERT INTO users (id, login, password_hash, full_name, role) VALUES ($1, $2, 'x', 'Тест', 'admin')
	`, id, id.String())
	require.NoError(t, err)
	return id
}

// createTestFile — file_id в parse_jobs ссылается на files.id.
func createTestFile(t *testing.T, database *db.DB, objectID, processID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := database.Pool().Exec(context.Background(), `
		INSERT INTO files (id, object_id, process_id, original_name, storage_key, size_bytes, check_status, is_current)
		VALUES ($1, $2, $3, 'test.pdf', 'key', 100, 'ACCEPTED', true)
	`, id, objectID, processID)
	require.NoError(t, err)
	return id
}

func TestService_Start_CreatesParseJobsAndOutbox(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()
	objectID, processID := createTestObjectAndProcess(t, database)
	fileID := createTestFile(t, database, objectID, processID)

	lister := fakeFileLister{files: []process.CurrentFile{{ID: fileID, DocStage: "PD", IsPDF: true, PageCount: 25}}}
	svc := process.NewService(database, lister, fakeFindingsGate{})

	p, err := svc.Start(ctx, processID)
	require.NoError(t, err)
	require.Equal(t, process.StatusParsing, p.Status)

	var jobCount int
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT count(*) FROM parse_jobs WHERE process_id = $1`, processID).Scan(&jobCount))
	require.Equal(t, 2, jobCount, "25 страниц -> 2 задания по 20")

	var outboxCount int
	require.NoError(t, database.Pool().QueryRow(ctx,
		`SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'doc.parse.requested'`, processID,
	).Scan(&outboxCount))
	require.Equal(t, 2, outboxCount)
}

func TestService_Start_WrongStatus(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()
	_, processID := createTestObjectAndProcess(t, database)

	_, err := database.Pool().Exec(ctx, `UPDATE processes SET status = 'READY' WHERE id = $1`, processID)
	require.NoError(t, err)

	svc := process.NewService(database, fakeFileLister{}, fakeFindingsGate{})
	_, err = svc.Start(ctx, processID)
	require.ErrorIs(t, err, process.ErrInvalidTransition)
}

func TestService_Start_NoAcceptedFiles(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()
	_, processID := createTestObjectAndProcess(t, database)

	svc := process.NewService(database, fakeFileLister{files: nil}, fakeFindingsGate{})
	_, err := svc.Start(ctx, processID)
	require.ErrorIs(t, err, process.ErrNoAcceptedFiles)

	var status string
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT status FROM processes WHERE id = $1`, processID).Scan(&status))
	require.Equal(t, process.StatusPending, status, "неудачный Start не должен менять статус (rollback)")
}

func TestService_Finalize_Success(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()
	_, processID := createTestObjectAndProcess(t, database)
	_, err := database.Pool().Exec(ctx, `UPDATE processes SET status = 'COMPLETED' WHERE id = $1`, processID)
	require.NoError(t, err)

	svc := process.NewService(database, fakeFileLister{}, fakeFindingsGate{pending: false})
	actorID := createTestUser(t, database)
	p, err := svc.Finalize(ctx, processID, actorID)
	require.NoError(t, err)
	require.Equal(t, process.StatusFinalized, p.Status)
	require.NotNil(t, p.FinalizedBy)
	require.Equal(t, actorID, *p.FinalizedBy)

	var auditCount int
	require.NoError(t, database.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE object_id = $1 AND action = 'process.finalize'`, processID.String(),
	).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

func TestService_Finalize_PendingCandidatesBlocks(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()
	_, processID := createTestObjectAndProcess(t, database)
	_, err := database.Pool().Exec(ctx, `UPDATE processes SET status = 'COMPLETED' WHERE id = $1`, processID)
	require.NoError(t, err)

	svc := process.NewService(database, fakeFileLister{}, fakeFindingsGate{pending: true})
	_, err = svc.Finalize(ctx, processID, uuid.New())
	require.ErrorIs(t, err, process.ErrHasPendingCandidates)

	var status string
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT status FROM processes WHERE id = $1`, processID).Scan(&status))
	require.Equal(t, process.StatusCompleted, status)
}

func TestService_Unfinalize(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()
	_, processID := createTestObjectAndProcess(t, database)
	_, err := database.Pool().Exec(ctx, `UPDATE processes SET status = 'FINALIZED', finalized_at = now() WHERE id = $1`, processID)
	require.NoError(t, err)

	svc := process.NewService(database, fakeFileLister{}, fakeFindingsGate{})

	_, err = svc.Unfinalize(ctx, processID, uuid.New(), "")
	require.ErrorIs(t, err, process.ErrReasonRequired)

	actorID := createTestUser(t, database)
	p, err := svc.Unfinalize(ctx, processID, actorID, "ошибочная финализация")
	require.NoError(t, err)
	require.Equal(t, process.StatusCompleted, p.Status)
	require.Nil(t, p.FinalizedAt)
}

func TestService_Unfinalize_WrongStatus(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()
	_, processID := createTestObjectAndProcess(t, database)

	svc := process.NewService(database, fakeFileLister{}, fakeFindingsGate{})
	_, err := svc.Unfinalize(ctx, processID, uuid.New(), "reason")
	require.ErrorIs(t, err, process.ErrInvalidTransition)
}

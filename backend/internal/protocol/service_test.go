package protocol_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/protocol"
)

func TestRepository_CreateVersionsIncrement(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	objectID := uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'Объект')`, objectID)
	require.NoError(t, err)
	processID := uuid.New()
	_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'READY')`, processID, objectID)
	require.NoError(t, err)

	repo := protocol.NewRepository(database.Pool())

	p1, err := repo.Create(ctx, protocol.Protocol{ID: uuid.New(), ProcessID: processID, ObjectID: objectID, Status: protocol.StatusDraft})
	require.NoError(t, err)
	require.Equal(t, 1, p1.Version)

	p2, err := repo.Create(ctx, protocol.Protocol{ID: uuid.New(), ProcessID: processID, ObjectID: objectID, Status: protocol.StatusDraft})
	require.NoError(t, err)
	require.Equal(t, 2, p2.Version)

	latest, err := repo.LatestByProcess(ctx, processID)
	require.NoError(t, err)
	require.Equal(t, 2, latest.Version)

	_, err = repo.LatestByProcess(ctx, uuid.New())
	require.ErrorIs(t, err, protocol.ErrNotFound)
}

func TestService_Get_AssemblesFindings(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	objectID := uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'Объект')`, objectID)
	require.NoError(t, err)
	processID := uuid.New()
	_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'READY')`, processID, objectID)
	require.NoError(t, err)

	_, err = protocol.NewRepository(database.Pool()).Create(ctx, protocol.Protocol{
		ID: uuid.New(), ProcessID: processID, ObjectID: objectID, Status: protocol.StatusDraft,
	})
	require.NoError(t, err)

	evidenceGroupID := uuid.New()
	_, err = database.Pool().Exec(ctx, `INSERT INTO evidence_groups (id, process_id, object_id, input_hash) VALUES ($1, $2, $3, 'h')`, evidenceGroupID, processID, objectID)
	require.NoError(t, err)
	checkID := uuid.New()
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO checks (id, process_id, object_id, evidence_group_id, finding_status) VALUES ($1, $2, $3, $4, 'CANDIDATE')
	`, checkID, processID, objectID, evidenceGroupID)
	require.NoError(t, err)
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO findings (id, check_id, finding_status, inspector_status) VALUES ($1, $2, 'CANDIDATE', 'PENDING')
	`, uuid.New(), checkID)
	require.NoError(t, err)

	svc := protocol.NewService(database)
	a, err := svc.Get(ctx, processID)
	require.NoError(t, err)
	require.Equal(t, 1, a.Protocol.Version)
	require.Len(t, a.Findings, 1)
}

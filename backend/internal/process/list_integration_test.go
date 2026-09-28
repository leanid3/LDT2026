package process_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/process"
)

func TestListByObject_NewestFirstAndScopedToObject(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	mkObject := func() uuid.UUID {
		id := uuid.New()
		_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'o')`, id)
		require.NoError(t, err)
		return id
	}
	mkProcess := func(obj uuid.UUID, age time.Duration) uuid.UUID {
		id := uuid.New()
		_, err := database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status, created_at) VALUES ($1, $2, 'PENDING', now() - $3::interval)`,
			id, obj, age.String())
		require.NoError(t, err)
		return id
	}
	a, b := mkObject(), mkObject()
	old, mid, fresh := mkProcess(a, 48*time.Hour), mkProcess(a, 24*time.Hour), mkProcess(a, time.Minute)
	other := mkProcess(b, time.Hour)

	got, err := process.NewRepository(database.Pool()).ListByObject(ctx, a)
	require.NoError(t, err)
	var ids []uuid.UUID
	for _, p := range got {
		ids = append(ids, p.ID)
	}
	require.Equal(t, []uuid.UUID{fresh, mid, old}, ids)
	require.NotContains(t, ids, other, "процессы чужого объекта не попадают в список")

	empty, err := process.NewRepository(database.Pool()).ListByObject(ctx, uuid.New())
	require.NoError(t, err)
	require.Empty(t, empty)
}

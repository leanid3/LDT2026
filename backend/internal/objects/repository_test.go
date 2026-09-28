package objects_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/objects"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
)

func TestRepository_CreateGetList(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()
	repo := objects.NewRepository(database.Pool())

	name := "ЖК Тестовый"
	created, err := repo.Create(ctx, objects.Object{ID: uuid.New(), Name: name})
	require.NoError(t, err)
	require.NotZero(t, created.CreatedAt)

	got, err := repo.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, name, got.Name)

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	_, err = repo.Get(ctx, uuid.New())
	require.ErrorIs(t, err, objects.ErrNotFound)
}

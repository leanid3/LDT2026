package idempotency_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/idempotency"
)

func TestOnce_SecondDeliveryIsNoOp(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	eventID := uuid.New().String()
	var calls int64
	fn := func(tx pgx.Tx) error {
		atomic.AddInt64(&calls, 1)
		return nil
	}

	handled1, err := idempotency.Once(ctx, database, "engine", eventID, fn)
	require.NoError(t, err)
	require.True(t, handled1)

	handled2, err := idempotency.Once(ctx, database, "engine", eventID, fn)
	require.NoError(t, err)
	require.False(t, handled2)

	require.EqualValues(t, 1, atomic.LoadInt64(&calls))
}

func TestOnce_DifferentConsumersAreIndependent(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	eventID := uuid.New().String()
	var calls int64
	fn := func(tx pgx.Tx) error {
		atomic.AddInt64(&calls, 1)
		return nil
	}

	_, err := idempotency.Once(ctx, database, "engine", eventID, fn)
	require.NoError(t, err)
	_, err = idempotency.Once(ctx, database, "rin-sync", eventID, fn)
	require.NoError(t, err)

	require.EqualValues(t, 2, atomic.LoadInt64(&calls))
}

func TestOnce_FnErrorRollsBackConsumedMarker(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	eventID := uuid.New().String()
	boom := errBoom{}
	_, err := idempotency.Once(ctx, database, "engine", eventID, func(tx pgx.Tx) error {
		return boom
	})
	require.ErrorIs(t, err, boom)

	// Транзакция откатилась целиком (включая INSERT consumed_events), значит повтор снова считается новым.
	var calls int
	handled, err := idempotency.Once(ctx, database, "engine", eventID, func(tx pgx.Tx) error {
		calls++
		return nil
	})
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, 1, calls)
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

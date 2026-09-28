package outbox_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/kafka/kafkatest"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/outbox"
)

func TestInsertAndRelay_PublishesAndMarksPublished(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	aggID := uuid.New()
	err := database.WithTx(ctx, func(tx pgx.Tx) error {
		return outbox.Insert(ctx, tx, outbox.Event{
			AggregateID: aggID,
			EventType:   "doc.parse.requested",
			Topic:       "doc.parse.requested",
			MsgKey:      aggID.String(),
			ObjectID:    aggID.String(),
			Data:        map[string]string{"file_id": "f-1"},
		})
	})
	require.NoError(t, err)

	producer := kafkatest.New()
	relay := outbox.NewRelay(database.Pool(), producer, slog.Default(), outbox.RelayConfig{})

	require.NoError(t, relay.Tick(ctx))

	msgs := producer.Messages()
	require.Len(t, msgs, 1)
	require.Equal(t, "doc.parse.requested", msgs[0].Topic)
	require.Equal(t, aggID.String(), msgs[0].Key)
	require.Equal(t, "doc.parse.requested", msgs[0].Headers["event_type"])

	var envelope struct {
		Data map[string]string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(msgs[0].Value, &envelope))
	require.Equal(t, "f-1", envelope.Data["file_id"])

	var status string
	err = database.Pool().QueryRow(ctx, `SELECT status FROM outbox_events WHERE aggregate_id = $1`, aggID).Scan(&status)
	require.NoError(t, err)
	require.Equal(t, "published", status)
}

func TestRelay_FailedPublishRetriesWithBackoff(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	aggID := uuid.New()
	require.NoError(t, database.WithTx(ctx, func(tx pgx.Tx) error {
		return outbox.Insert(ctx, tx, outbox.Event{
			AggregateID: aggID,
			EventType:   "doc.parse.requested",
			Topic:       "doc.parse.requested",
			MsgKey:      aggID.String(),
			Data:        map[string]string{},
		})
	}))

	relay := outbox.NewRelay(database.Pool(), failingSender{}, slog.Default(), outbox.RelayConfig{})
	require.NoError(t, relay.Tick(ctx))

	var status string
	var attempts int
	var availableAt time.Time
	err := database.Pool().QueryRow(ctx,
		`SELECT status, attempts, available_at FROM outbox_events WHERE aggregate_id = $1`, aggID,
	).Scan(&status, &attempts, &availableAt)
	require.NoError(t, err)
	require.Equal(t, "failed", status)
	require.Equal(t, 1, attempts)
	require.True(t, availableAt.After(time.Now()))
}

func TestRelay_StuckPublishingIsRequeued(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	aggID := uuid.New()
	require.NoError(t, database.WithTx(ctx, func(tx pgx.Tx) error {
		return outbox.Insert(ctx, tx, outbox.Event{
			AggregateID: aggID, EventType: "x", Topic: "x", MsgKey: "k", Data: map[string]string{},
		})
	}))
	_, err := database.Pool().Exec(ctx,
		`UPDATE outbox_events SET status = 'publishing', locked_at = now() - interval '10 minutes' WHERE aggregate_id = $1`,
		aggID)
	require.NoError(t, err)

	producer := kafkatest.New()
	relay := outbox.NewRelay(database.Pool(), producer, slog.Default(), outbox.RelayConfig{})
	require.NoError(t, relay.Tick(ctx))

	require.Len(t, producer.Messages(), 1, "stuck publishing event should have been requeued and published")
}

type failingSender struct{}

func (failingSender) SendRaw(context.Context, string, string, map[string]string, []byte) error {
	return errBoom
}

var errBoom = errBoomType{}

type errBoomType struct{}

func (errBoomType) Error() string { return "boom" }

package mockworkers_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/google/uuid"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/mockworkers"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
)

func kafkaMessage(t *testing.T, topic string, envelope map[string]any) *kafka.Message {
	t.Helper()
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	return &kafka.Message{TopicPartition: kafka.TopicPartition{Topic: &topic}, Value: data}
}

func TestHandler_ParseRequested_PublishesCompleted(t *testing.T) {
	database := dbtest.NewPostgres(t)
	minioClient := dbtest.NewMinio(t)
	ctx := context.Background()

	processID, objectID := uuid.New(), uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'Объект')`, objectID)
	require.NoError(t, err)
	_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'PARSING')`, processID, objectID)
	require.NoError(t, err)

	h := mockworkers.NewHandler(database, minioClient, slog.Default())

	jobID := uuid.New()
	msg := kafkaMessage(t, "doc.parse.requested", map[string]any{
		"event_id": uuid.New().String(), "event_type": "doc.parse.requested",
		"object_id": objectID.String(), "process_id": processID.String(),
		"data": map[string]any{"job_id": jobID.String(), "file_id": uuid.New().String(), "doc_stage": "PD"},
	})
	require.NoError(t, h.Handle(ctx, msg))

	var count int
	require.NoError(t, database.Pool().QueryRow(ctx,
		`SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'doc.parse.completed'`, processID,
	).Scan(&count))
	require.Equal(t, 1, count)
}

func TestHandler_ExtractRequested_UploadsFactsAndPublishesCompleted(t *testing.T) {
	database := dbtest.NewPostgres(t)
	minioClient := dbtest.NewMinio(t)
	ctx := context.Background()

	processID, objectID := uuid.New(), uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'Объект')`, objectID)
	require.NoError(t, err)
	_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'PARSING')`, processID, objectID)
	require.NoError(t, err)

	h := mockworkers.NewHandler(database, minioClient, slog.Default())

	jobID := uuid.New()
	fileID := uuid.New()
	msg := kafkaMessage(t, "doc.extract.requested", map[string]any{
		"event_id": uuid.New().String(), "event_type": "doc.extract.requested",
		"object_id": objectID.String(), "process_id": processID.String(),
		"data": map[string]any{"job_id": jobID.String(), "files": []map[string]string{{"file_id": fileID.String(), "stage": "PD"}}},
	})
	require.NoError(t, h.Handle(ctx, msg))

	var payload struct {
		Data struct {
			FactsKey string `json:"facts_key"`
		} `json:"data"`
	}
	var raw []byte
	require.NoError(t, database.Pool().QueryRow(ctx,
		`SELECT payload FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'doc.extract.completed'`, processID,
	).Scan(&raw))
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.NotEmpty(t, payload.Data.FactsKey)

	obj, err := minioClient.Raw().GetObject(ctx, minioClient.BucketName(), payload.Data.FactsKey, miniogo.GetObjectOptions{})
	require.NoError(t, err)
	defer func() { _ = obj.Close() }()

	info, err := obj.Stat()
	require.NoError(t, err)
	require.Greater(t, info.Size, int64(0), "facts JSONL должен быть не пустым")
}

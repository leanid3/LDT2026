package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/google/uuid"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine/rules"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
)

func kafkaMessage(t *testing.T, topic string, envelope map[string]any) *kafka.Message {
	t.Helper()
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	return &kafka.Message{TopicPartition: kafka.TopicPartition{Topic: &topic}, Value: data}
}

func TestHandler_ParseCompleted_FanInToExtract_WhenAllJobsDone(t *testing.T) {
	database := dbtest.NewPostgres(t)
	minioClient := dbtest.NewMinio(t)
	ctx := context.Background()

	objectID := uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'Объект')`, objectID)
	require.NoError(t, err)
	processID := uuid.New()
	_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'PARSING')`, processID, objectID)
	require.NoError(t, err)
	fileID := uuid.New()
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO files (id, object_id, process_id, original_name, storage_key, size_bytes, check_status, is_current)
		VALUES ($1, $2, $3, 'a.pdf', 'k', 1, 'ACCEPTED', true)
	`, fileID, objectID, processID)
	require.NoError(t, err)

	job1, job2 := uuid.New(), uuid.New()
	_, err = database.Pool().Exec(ctx, `INSERT INTO parse_jobs (id, process_id, file_id, status) VALUES ($1,$2,$3,'PENDING'), ($4,$2,$3,'PENDING')`,
		job1, processID, fileID, job2)
	require.NoError(t, err)

	h := engine.NewHandler(database, minioClient, nil)

	// первое задание готово — fan-in ещё не должен сработать (второе всё ещё PENDING)
	msg1 := kafkaMessage(t, "doc.parse.completed", map[string]any{
		"event_id": uuid.New().String(), "event_type": "doc.parse.completed",
		"object_id": objectID.String(), "process_id": processID.String(),
		"data": map[string]any{"job_id": job1.String(), "quality": "OK", "layout_key": "layout-1"},
	})
	require.NoError(t, h.Handle(ctx, msg1))

	var extractCount int
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT count(*) FROM extract_jobs WHERE process_id = $1`, processID).Scan(&extractCount))
	require.Zero(t, extractCount, "fan-in не должен сработать, пока не все parse_jobs готовы")

	// второе задание готово — fan-in должен создать extract_job + outbox doc.extract.requested
	msg2 := kafkaMessage(t, "doc.parse.completed", map[string]any{
		"event_id": uuid.New().String(), "event_type": "doc.parse.completed",
		"object_id": objectID.String(), "process_id": processID.String(),
		"data": map[string]any{"job_id": job2.String(), "quality": "OK", "layout_key": "layout-2"},
	})
	require.NoError(t, h.Handle(ctx, msg2))

	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT count(*) FROM extract_jobs WHERE process_id = $1`, processID).Scan(&extractCount))
	require.Equal(t, 1, extractCount)

	var outboxCount int
	require.NoError(t, database.Pool().QueryRow(ctx,
		`SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'doc.extract.requested'`, processID,
	).Scan(&outboxCount))
	require.Equal(t, 1, outboxCount)
}

func TestHandler_ExtractCompleted_ProcessesFactsAndReachesReady(t *testing.T) {
	database := dbtest.NewPostgres(t)
	minioClient := dbtest.NewMinio(t)
	ctx := context.Background()

	objectID := uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'Объект')`, objectID)
	require.NoError(t, err)
	processID := uuid.New()
	_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'PARSING')`, processID, objectID)
	require.NoError(t, err)
	fileID := uuid.New()
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO files (id, object_id, process_id, original_name, storage_key, size_bytes, check_status, is_current)
		VALUES ($1, $2, $3, 'a.pdf', 'k', 1, 'ACCEPTED', true)
	`, fileID, objectID, processID)
	require.NoError(t, err)

	extractJobID := uuid.New()
	_, err = database.Pool().Exec(ctx, `INSERT INTO extract_jobs (id, process_id, status) VALUES ($1, $2, 'PENDING')`, extractJobID, processID)
	require.NoError(t, err)

	// JSONL с фактами в MinIO — как это делал бы LLM-воркер (или cmd/mockworkers).
	factsKey := "facts/" + extractJobID.String() + ".jsonl"
	var buf bytes.Buffer
	for _, f := range []map[string]any{
		{"fact_id": uuid.New().String(), "file_id": fileID.String(), "stage": "PD", "param_code": "DEMO-001", "value_raw": "100", "quote": "x"},
		{"fact_id": uuid.New().String(), "file_id": fileID.String(), "stage": "RD", "param_code": "DEMO-001", "value_raw": "100", "quote": "y"},
	} {
		line, _ := json.Marshal(f)
		buf.Write(line)
		buf.WriteByte('\n')
	}
	_, err = minioClient.Raw().PutObject(ctx, minioClient.BucketName(), factsKey, &buf, int64(buf.Len()), miniogo.PutObjectOptions{})
	require.NoError(t, err)

	ruleSet := map[string]rules.Rule{"DEMO-001": {Code: "DEMO-001", Type: rules.TypeNumericEqual}}
	h := engine.NewHandler(database, minioClient, ruleSet)

	msg := kafkaMessage(t, "doc.extract.completed", map[string]any{
		"event_id": uuid.New().String(), "event_type": "doc.extract.completed",
		"object_id": objectID.String(), "process_id": processID.String(),
		"data": map[string]any{"job_id": extractJobID.String(), "facts_key": factsKey},
	})
	require.NoError(t, h.Handle(ctx, msg))

	var status string
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT status FROM processes WHERE id = $1`, processID).Scan(&status))
	require.Equal(t, "READY", status)

	var factCount, checkCount, protocolCount int
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT count(*) FROM facts WHERE process_id = $1`, processID).Scan(&factCount))
	require.Equal(t, 2, factCount)
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT count(*) FROM checks WHERE process_id = $1`, processID).Scan(&checkCount))
	require.Equal(t, 1, checkCount)
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT count(*) FROM protocols WHERE process_id = $1`, processID).Scan(&protocolCount))
	require.Equal(t, 1, protocolCount)

	// повторная доставка того же event_id идемпотентна — не должно быть дублей.
	require.NoError(t, h.Handle(ctx, msg))
	require.NoError(t, database.Pool().QueryRow(ctx, `SELECT count(*) FROM facts WHERE process_id = $1`, processID).Scan(&factCount))
	require.Equal(t, 2, factCount, "повторная доставка не должна дублировать факты")
}

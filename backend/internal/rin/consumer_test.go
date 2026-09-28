package rin_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/rin"
)

func kafkaMessage(t *testing.T, topic string, envelope map[string]any) *kafka.Message {
	t.Helper()
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	return &kafka.Message{TopicPartition: kafka.TopicPartition{Topic: &topic}, Value: data}
}

func setupProcessWithProtocol(t *testing.T, database *db.DB) (objectID, processID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	objectID = uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'Объект')`, objectID)
	require.NoError(t, err)
	processID = uuid.New()
	_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'FINALIZED')`, processID, objectID)
	require.NoError(t, err)
	_, err = database.Pool().Exec(ctx, `
		INSERT INTO protocols (id, process_id, object_id, version, status, sync_status) VALUES ($1, $2, $3, 1, 'PROTOCOL_FINALIZED', 'PENDING_SYNC')
	`, uuid.New(), processID, objectID)
	require.NoError(t, err)
	return objectID, processID
}

func TestHandler_Sync_SuccessOnFirstTry(t *testing.T) {
	database := dbtest.NewPostgres(t)
	objectID, processID := setupProcessWithProtocol(t, database)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	h := rin.NewHandler(database, rin.NoopSigner{}, rin.Config{Endpoint: server.URL, Timeout: 2 * time.Second}, slog.Default())

	msg := kafkaMessage(t, "rin.sync.requested", map[string]any{
		"event_id": uuid.New().String(), "event_type": "rin.sync.requested",
		"object_id": objectID.String(), "process_id": processID.String(),
		"data": map[string]any{"process_id": processID.String()},
	})
	require.NoError(t, h.Handle(context.Background(), msg))

	var status string
	require.NoError(t, database.Pool().QueryRow(context.Background(),
		`SELECT sync_status FROM protocols WHERE process_id = $1`, processID).Scan(&status))
	require.Equal(t, "SYNCED", status)
}

func TestHandler_Sync_RetriesThenSucceeds(t *testing.T) {
	database := dbtest.NewPostgres(t)
	objectID, processID := setupProcessWithProtocol(t, database)

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	h := rin.NewHandler(database, rin.NoopSigner{}, rin.Config{
		Endpoint: server.URL, Timeout: 2 * time.Second,
		RetryDelays: []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond},
	}, slog.Default())

	msg := kafkaMessage(t, "rin.sync.requested", map[string]any{
		"event_id": uuid.New().String(), "event_type": "rin.sync.requested",
		"object_id": objectID.String(), "process_id": processID.String(),
		"data": map[string]any{"process_id": processID.String()},
	})
	require.NoError(t, h.Handle(context.Background(), msg))
	require.EqualValues(t, 3, atomic.LoadInt32(&calls))

	var status string
	require.NoError(t, database.Pool().QueryRow(context.Background(),
		`SELECT sync_status FROM protocols WHERE process_id = $1`, processID).Scan(&status))
	require.Equal(t, "SYNCED", status)
}

func TestHandler_Sync_ExhaustsRetries_MarksFailed(t *testing.T) {
	database := dbtest.NewPostgres(t)
	objectID, processID := setupProcessWithProtocol(t, database)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	h := rin.NewHandler(database, rin.NoopSigner{}, rin.Config{
		Endpoint: server.URL, Timeout: 2 * time.Second,
		RetryDelays: []time.Duration{5 * time.Millisecond, 5 * time.Millisecond},
	}, slog.Default())

	msg := kafkaMessage(t, "rin.sync.requested", map[string]any{
		"event_id": uuid.New().String(), "event_type": "rin.sync.requested",
		"object_id": objectID.String(), "process_id": processID.String(),
		"data": map[string]any{"process_id": processID.String()},
	})
	require.NoError(t, h.Handle(context.Background(), msg))

	var status string
	require.NoError(t, database.Pool().QueryRow(context.Background(),
		`SELECT sync_status FROM protocols WHERE process_id = $1`, processID).Scan(&status))
	require.Equal(t, "SYNC_FAILED", status)
}

func TestHandler_Sync_ClientErrorNotRetried(t *testing.T) {
	database := dbtest.NewPostgres(t)
	objectID, processID := setupProcessWithProtocol(t, database)

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	h := rin.NewHandler(database, rin.NoopSigner{}, rin.Config{
		Endpoint: server.URL, Timeout: 2 * time.Second,
		RetryDelays: []time.Duration{5 * time.Millisecond, 5 * time.Millisecond},
	}, slog.Default())

	msg := kafkaMessage(t, "rin.sync.requested", map[string]any{
		"event_id": uuid.New().String(), "event_type": "rin.sync.requested",
		"object_id": objectID.String(), "process_id": processID.String(),
		"data": map[string]any{"process_id": processID.String()},
	})
	require.NoError(t, h.Handle(context.Background(), msg))
	require.EqualValues(t, 1, atomic.LoadInt32(&calls), "4xx не должен ретраиться")

	var status string
	require.NoError(t, database.Pool().QueryRow(context.Background(),
		`SELECT sync_status FROM protocols WHERE process_id = $1`, processID).Scan(&status))
	require.Equal(t, "SYNC_FAILED", status)
}

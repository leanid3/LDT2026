package kafka

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/stretchr/testify/require"
)

type fakeHandler struct {
	failTimes int
	calls     int
}

func (h *fakeHandler) Handle(context.Context, *kafka.Message) error {
	h.calls++
	if h.calls <= h.failTimes {
		return errors.New("boom")
	}
	return nil
}

type dlqRecord struct {
	topic, key string
	headers    map[string]string
	value      []byte
}

type fakeDLQ struct {
	mu       sync.Mutex
	failNext int
	got      []dlqRecord
}

func (d *fakeDLQ) SendRaw(_ context.Context, topic, key string, headers map[string]string, value []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failNext > 0 {
		d.failNext--
		return errors.New("kafka down")
	}
	d.got = append(d.got, dlqRecord{topic, key, headers, value})
	return nil
}

func newTestConsumer(h Handler, dlq DLQSender) *consumer {
	cfg := ConsumerConfig{MaxAttempts: 3, RetryBackoff: time.Millisecond, DLQ: dlq}.withDefaults()
	return &consumer{handler: h, cfg: cfg, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func msg(topic string) *kafka.Message {
	return &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: 2, Offset: 41},
		Key:            []byte("object-1"), Value: []byte(`{"event_id":"e1"}`),
		Headers: []kafka.Header{{Key: "correlation_id", Value: []byte("p1")}},
	}
}

func TestProcessWithRetry_SuccessFirstTry(t *testing.T) {
	h, dlq := &fakeHandler{}, &fakeDLQ{}
	require.True(t, newTestConsumer(h, dlq).processWithRetry(context.Background(), msg("doc.parse.completed")))
	require.Equal(t, 1, h.calls)
	require.Empty(t, dlq.got)
}

func TestProcessWithRetry_TransientFailureIsRetried(t *testing.T) {
	h, dlq := &fakeHandler{failTimes: 2}, &fakeDLQ{}
	require.True(t, newTestConsumer(h, dlq).processWithRetry(context.Background(), msg("doc.parse.completed")))
	require.Equal(t, 3, h.calls, "две неудачи и успех на третьей попытке")
	require.Empty(t, dlq.got, "восстановившееся сообщение не должно попасть в DLQ")
}

func TestProcessWithRetry_ExhaustedGoesToDLQWithContext(t *testing.T) {
	h, dlq := &fakeHandler{failTimes: 100}, &fakeDLQ{}
	require.True(t, newTestConsumer(h, dlq).processWithRetry(context.Background(), msg("doc.parse.completed")))
	require.Equal(t, 3, h.calls)
	require.Len(t, dlq.got, 1)

	got := dlq.got[0]
	require.Equal(t, "doc.parse.completed.dlq", got.topic)
	require.Equal(t, "object-1", got.key)
	require.JSONEq(t, `{"event_id":"e1"}`, string(got.value), "исходное тело сохраняется как есть — для replay")
	require.Equal(t, "boom", got.headers["dlq_error"])
	require.Equal(t, "doc.parse.completed", got.headers["dlq_source_topic"])
	require.Equal(t, "2", got.headers["dlq_source_partition"])
	require.Equal(t, "41", got.headers["dlq_source_offset"])
	require.Equal(t, "p1", got.headers["correlation_id"], "исходные заголовки не теряются")
}

func TestProcessWithRetry_DLQFailureIsRetriedNotDropped(t *testing.T) {
	h, dlq := &fakeHandler{failTimes: 100}, &fakeDLQ{failNext: 1}
	c := newTestConsumer(h, dlq)
	done := make(chan bool, 1)
	go func() { done <- c.processWithRetry(context.Background(), msg("t")) }()
	select {
	case ok := <-done:
		require.True(t, ok)
	case <-time.After(10 * time.Second):
		t.Fatal("processWithRetry завис")
	}
	require.Len(t, dlq.got, 1, "первая отправка в DLQ упала — сообщение не должно потеряться")
}

func TestProcessWithRetry_NoDLQConfiguredDropsWithoutPanic(t *testing.T) {
	h := &fakeHandler{failTimes: 100}
	require.True(t, newTestConsumer(h, nil).processWithRetry(context.Background(), msg("t")))
	require.Equal(t, 3, h.calls)
}

func TestProcessWithRetry_CancelledContextDoesNotAckMessage(t *testing.T) {
	h, dlq := &fakeHandler{failTimes: 100}, &fakeDLQ{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.False(t, newTestConsumer(h, dlq).processWithRetry(ctx, msg("t")), "остановка сервиса — не повод отправлять сообщение в DLQ")
	require.Empty(t, dlq.got)
}

// Package kafkatest даёт in-memory реализацию kafka.Producer для юнит/интеграционных тестов —
// без поднятия реального брокера.
package kafkatest

import (
	"context"
	"encoding/json"
	"sync"
)

type Message struct {
	Topic   string
	Key     string
	Headers map[string]string
	Value   []byte
}

// FakeProducer собирает отправленные сообщения в памяти, потокобезопасно.
type FakeProducer struct {
	mu       sync.Mutex
	messages []Message
	closed   bool
}

func New() *FakeProducer {
	return &FakeProducer{}
}

func (f *FakeProducer) Send(_ context.Context, topic, key string, headers map[string]string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return f.SendRaw(context.Background(), topic, key, headers, data)
}

func (f *FakeProducer) SendRaw(_ context.Context, topic, key string, headers map[string]string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, Message{Topic: topic, Key: key, Headers: headers, Value: append([]byte(nil), data...)})
	return nil
}

func (f *FakeProducer) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *FakeProducer) Messages() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Message, len(f.messages))
	copy(out, f.messages)
	return out
}

func (f *FakeProducer) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

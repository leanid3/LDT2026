package mockworkers

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/events"
)

// Контрактные тесты: те же файлы contracts/events/examples/*.json Python-воркеры проверяют по
// JSON Schema. Здесь — строгий разбор в Go-структуры (DisallowUnknownFields), чтобы поле, добавленное в
// контракт, но не поддержанное оркестратором, ломало сборку, а не терялось молча.

func loadExample(t *testing.T, name string) events.Envelope {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "contracts", "events", "examples", name+".json"))
	require.NoError(t, err)
	var env events.Envelope
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, name, env.EventType)
	return env
}

func decodeStrict(t *testing.T, raw json.RawMessage, into any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	require.NoError(t, dec.Decode(into))
}

func TestContract_ParseRequested(t *testing.T) {
	var p ParseRequestedPayload
	decodeStrict(t, loadExample(t, "doc.parse.requested").Data, &p)
	require.NotEmpty(t, p.StorageKey)
	require.NotNil(t, p.SHA256)
	require.Equal(t, 1, *p.PageFrom)
}

func TestContract_ParseCompleted(t *testing.T) {
	var p engine.ParseCompletedPayload
	decodeStrict(t, loadExample(t, "doc.parse.completed").Data, &p)
	require.Equal(t, "OK", p.Quality)
	require.NotEmpty(t, p.LayoutKey)
}

func TestContract_ExtractRequested(t *testing.T) {
	var p ExtractRequestedPayload
	decodeStrict(t, loadExample(t, "doc.extract.requested").Data, &p)
	require.Len(t, p.Files, 1)
	require.Len(t, p.Files[0].LayoutKeys, 1)
}

func TestContract_ExtractCompleted(t *testing.T) {
	var p engine.ExtractCompletedPayload
	decodeStrict(t, loadExample(t, "doc.extract.completed").Data, &p)
	require.NotEmpty(t, p.FactsKey)
}

func TestContract_Fact(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "contracts", "events", "examples", "fact.json"))
	require.NoError(t, err)
	var f engine.Fact
	decodeStrict(t, raw, &f)
	require.Equal(t, "M-001", f.ParamCode)
	require.JSONEq(t, `{"kind":"number","value":1250.5,"unit_si":"м²"}`, string(f.ValueNorm))
}

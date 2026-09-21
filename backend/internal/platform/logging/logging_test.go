package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew_JSONFieldsAndLevel(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: parseLevel("warn"),
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Key = "timestamp"
			}
			return a
		},
	})
	log := slog.New(handler).With("service", "api")

	log.Info("should be filtered out by warn level")
	log.Error("boom", "request_id", "abc")

	var lines []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal(line, &m))
		lines = append(lines, m)
	}

	require.Len(t, lines, 1, "info line should be filtered by warn level")
	require.Equal(t, "boom", lines[0]["msg"])
	require.Equal(t, "api", lines[0]["service"])
	require.Equal(t, "abc", lines[0]["request_id"])
	require.Contains(t, lines[0], "timestamp")
}

func TestParseLevel(t *testing.T) {
	require.Equal(t, slog.LevelDebug, parseLevel("debug"))
	require.Equal(t, slog.LevelWarn, parseLevel("WARN"))
	require.Equal(t, slog.LevelError, parseLevel("error"))
	require.Equal(t, slog.LevelInfo, parseLevel("unknown"))
}

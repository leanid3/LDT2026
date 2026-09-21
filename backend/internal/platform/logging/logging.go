// Package logging предоставляет единый JSON-логгер (slog) в stdout.
//
// По ТЗ (backend-plan.md §10): поля timestamp, level, service, message, request_id, user_id;
// файловый режим и ротация не используются — только stdout.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New создаёт JSON-логгер в stdout с полем service и уровнем level (debug|info|warn|error).
func New(level, service string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: parseLevel(level),
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Key = "timestamp"
			}
			return a
		},
	}

	handler := slog.NewJSONHandler(os.Stdout, opts)
	return slog.New(handler).With("service", service)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

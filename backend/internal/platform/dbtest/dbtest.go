// Package dbtest поднимает одноразовый Postgres в testcontainers и применяет миграции —
// общий хелпер для интеграционных тестов доменных пакетов (testify + testcontainers-go, как
// зафиксировано в backend/CLAUDE.md).
package dbtest

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
)

// NewPostgres поднимает контейнер postgres:16-alpine, применяет все миграции backend/migrations и
// возвращает готовый *db.DB. Пропускает тест в режиме `go test -short` (testcontainers — не для
// быстрого прогона, см. docs/development.md#тесты).
func NewPostgres(t *testing.T) *db.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("testcontainers: пропущено в -short режиме")
	}

	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("inspector"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	database, err := db.New(ctx, db.Config{
		Host:            host,
		Port:            port.Int(),
		User:            "postgres",
		Password:        "postgres",
		Database:        "inspector",
		MaxConnections:  10,
		MinConnections:  1,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	require.NoError(t, err)
	t.Cleanup(database.Close)

	applyMigrations(t, database)
	return database
}

// applyMigrations читает backend/migrations/*.up.sql в порядке имён файлов и выполняет их напрямую —
// проще и быстрее, чем поднимать ещё и golang-migrate CLI-образ внутри теста; сами миграции уже
// проверены вручную (docs/development.md#миграции).
func applyMigrations(t *testing.T, database *db.DB) {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "migrations")

	entries, err := os.ReadDir(migrationsDir)
	require.NoError(t, err)

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	require.NotEmpty(t, files, "no migrations found in %s", migrationsDir)

	ctx := context.Background()
	for _, name := range files {
		sql, err := os.ReadFile(filepath.Join(migrationsDir, name))
		require.NoError(t, err)
		_, err = database.Pool().Exec(ctx, string(sql))
		require.NoError(t, err, "apply migration %s", name)
	}
}

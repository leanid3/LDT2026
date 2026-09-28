package dbtest

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	miniomodule "github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/minio"
)

// MinioImage — образ MinIO для тестов и стенда. minio/minio и minio/mc удалены с Docker Hub (MinIO перестал
// публиковать готовые образы), quay.io/minio требует авторизации; cgr.dev/chainguard/minio — тот же MinIO,
// собранный из исходников Chainguard, анонимный pull. Синхронизировать с infra/docker-compose.yml.
const MinioImage = "cgr.dev/chainguard/minio:latest"

// NewMinio поднимает контейнер MinIO и возвращает готовый *minio.Client с созданным бакетом —
// для интеграционных тестов upload/confirm (files.Service), где нужен настоящий S3-совместимый API
// (presigned POST, GetObject), не только моки.
func NewMinio(t *testing.T) *minio.Client {
	t.Helper()
	return NewMinioPublic(t, "")
}

// NewMinioPublic — как NewMinio, но presigned-ссылки подписываются под publicEndpoint (host:port, как его
// видит браузер), а не под реальный адрес контейнера.
func NewMinioPublic(t *testing.T, publicEndpoint string) *minio.Client {
	t.Helper()
	if testing.Short() {
		t.Skip("testcontainers: пропущено в -short режиме")
	}

	ctx := context.Background()
	container, err := miniomodule.Run(ctx, MinioImage)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	endpoint, err := container.ConnectionString(ctx)
	require.NoError(t, err)

	client, err := minio.New(ctx, minio.Config{
		Endpoint: endpoint, AccessKey: container.Username, SecretKey: container.Password,
		Bucket: "documents-test", Region: "us-east-1", Timeout: 30 * time.Second, PublicEndpoint: publicEndpoint,
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	require.NoError(t, err)

	return client
}

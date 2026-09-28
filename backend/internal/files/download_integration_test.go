package files_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/files"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
)

func TestDownloadURL_ServesAcceptedFileAndRefusesOthers(t *testing.T) {
	database := dbtest.NewPostgres(t)
	minioClient := dbtest.NewMinio(t)
	ctx := context.Background()

	objectID, processID := uuid.New(), uuid.New()
	_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'o')`, objectID)
	require.NoError(t, err)
	_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'PENDING')`, processID, objectID)
	require.NoError(t, err)

	body := []byte("%PDF-1.4 содержимое документа")
	key := "objects/" + objectID.String() + "/doc.pdf"
	_, err = minioClient.Raw().PutObject(ctx, minioClient.BucketName(), key, bytes.NewReader(body), int64(len(body)), miniogo.PutObjectOptions{})
	require.NoError(t, err)

	insert := func(status string) uuid.UUID {
		id := uuid.New()
		_, err := database.Pool().Exec(ctx, `
			INSERT INTO files (id, object_id, process_id, original_name, storage_key, size_bytes, check_status)
			VALUES ($1, $2, $3, 'Проект ПД.pdf', $4, $5, $6)`, id, objectID, processID, key, len(body), status)
		require.NoError(t, err)
		return id
	}

	svc := files.NewService(database, files.NewRepository(database.Pool()), minioClient, nil, nil)

	accepted := insert("ACCEPTED")
	before := time.Now()
	u, expires, err := svc.DownloadURL(ctx, accepted)
	require.NoError(t, err)
	require.True(t, expires.After(before.Add(10*time.Minute)), "ссылка живёт минуты, не секунды")

	resp, err := http.Get(u) //nolint:gosec,noctx // тест против локального контейнера MinIO
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, body, got, "по presigned-ссылке отдаётся сам файл")
	require.Contains(t, resp.Header.Get("Content-Disposition"), "inline", "просмотр в браузере, не принудительная загрузка")

	for _, status := range []string{"UPLOADING", "REJECTED_VIRUS", "REJECTED_CORRUPTED"} {
		_, _, err := svc.DownloadURL(ctx, insert(status))
		require.ErrorIs(t, err, files.ErrNotDownloadable, status)
	}
	_, _, err = svc.DownloadURL(ctx, uuid.New())
	require.ErrorIs(t, err, files.ErrNotFound)
}

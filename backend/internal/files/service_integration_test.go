package files_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/files"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/clamav"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
)

// fakeClamd — сервер, всегда отвечающий "чисто" (полноценный протокол проверен в
// internal/platform/clamav; здесь просто заглушка, чтобы не тащить реальный ClamAV в этот тест).
func fakeClamd(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				r := bufio.NewReader(conn)
				if _, err := r.ReadString('\x00'); err != nil {
					return
				}
				lenBuf := make([]byte, 4)
				for {
					if _, err := io.ReadFull(r, lenBuf); err != nil {
						return
					}
					if binary.BigEndian.Uint32(lenBuf) == 0 {
						break
					}
					if _, err := io.CopyN(io.Discard, r, int64(binary.BigEndian.Uint32(lenBuf))); err != nil {
						return
					}
				}
				_, _ = conn.Write([]byte("stream: OK\x00"))
			}()
		}
	}()
	return ln.Addr().String()
}

type fakeProcessGate struct{}

// EnsureUploadable — упрощённая замена process.Service для тестов files: сама создаёт строку
// processes в переданной транзакции (объект должен существовать заранее — см. createTestObject).
func (fakeProcessGate) EnsureUploadable(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, processID *uuid.UUID, _ uuid.UUID) (uuid.UUID, error) {
	if processID != nil {
		return *processID, nil
	}
	pid := uuid.New()
	_, err := tx.Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'PENDING')`, pid, objectID)
	return pid, err
}

func (fakeProcessGate) UpdateScenario(context.Context, pgx.Tx, uuid.UUID, string) error { return nil }

func newTestService(t *testing.T) (*files.Service, *db.DB) {
	t.Helper()
	database := dbtest.NewPostgres(t)
	minioClient := dbtest.NewMinio(t)
	clamavClient := clamav.New(clamav.Config{Address: fakeClamd(t), Timeout: 5 * time.Second})

	repo := files.NewRepository(database.Pool())
	return files.NewService(database, repo, minioClient, clamavClient, fakeProcessGate{}), database
}

func createTestObject(t *testing.T, database *db.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := database.Pool().Exec(context.Background(), `INSERT INTO objects (id, name) VALUES ($1, 'Тестовый объект')`, id)
	require.NoError(t, err)
	return id
}

// doPresignedUpload выполняет реальный multipart POST по presigned-URL — так же, как это делал бы
// браузер/клиент (backend-plan.md §8.1.1: presigned POST с SetContentLengthRange).
func doPresignedUpload(t *testing.T, upload files.PresignedUpload, content []byte) {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range upload.UploadFields {
		require.NoError(t, w.WriteField(k, v))
	}
	part, err := w.CreateFormFile("file", upload.OriginalName)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req, err := http.NewRequest(http.MethodPost, upload.UploadURL, &body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	require.Truef(t, resp.StatusCode >= 200 && resp.StatusCode < 300,
		"presigned upload failed: %d %s", resp.StatusCode, string(respBody))
}

func TestService_UploadConfirm_HappyPath_PDF(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()

	pdf, err := os.ReadFile("testdata/sample.pdf")
	require.NoError(t, err)

	objectID := createTestObject(t, database)
	processID, uploads, err := svc.Upload(ctx, objectID, nil, uuid.New(), []files.UploadFileRequest{
		{OriginalName: "sample.pdf", SizeBytes: int64(len(pdf))},
	})
	require.NoError(t, err)
	require.Len(t, uploads, 1)

	doPresignedUpload(t, uploads[0], pdf)

	results, err := svc.Confirm(ctx, processID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, files.CheckStatusAccepted, results[0].CheckStatus)
	require.Equal(t, 1, results[0].PageCount)
	require.Empty(t, results[0].CheckError)

	// Повторный confirm идемпотентен (файл уже не в UPLOADING/UPLOADED).
	again, err := svc.Confirm(ctx, processID)
	require.NoError(t, err)
	require.Empty(t, again)
}

func TestService_UploadConfirm_DuplicateSHA256InProcess(t *testing.T) {
	svc, database := newTestService(t)
	ctx := context.Background()

	pdf, err := os.ReadFile("testdata/sample.pdf")
	require.NoError(t, err)

	objectID := createTestObject(t, database)
	processID, uploads, err := svc.Upload(ctx, objectID, nil, uuid.New(), []files.UploadFileRequest{
		{OriginalName: "a.pdf", SizeBytes: int64(len(pdf))},
		{OriginalName: "b-copy.pdf", SizeBytes: int64(len(pdf))},
	})
	require.NoError(t, err)
	require.Len(t, uploads, 2)

	doPresignedUpload(t, uploads[0], pdf)
	doPresignedUpload(t, uploads[1], pdf) // тот же контент — дубликат по sha256

	results, err := svc.Confirm(ctx, processID)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, r := range results {
		require.Equal(t, files.CheckStatusAccepted, r.CheckStatus, "оба файла принимаются, дубликат не блокирует загрузку")
	}
}

func TestService_Upload_RejectsOversizedFile(t *testing.T) {
	svc, _ := newTestService(t)
	_, _, err := svc.Upload(context.Background(), uuid.New(), nil, uuid.New(), []files.UploadFileRequest{
		{OriginalName: "huge.pdf", SizeBytes: files.MaxFileSizeBytes + 1},
	})
	require.ErrorIs(t, err, files.ErrValidation)
}

func TestService_Upload_RejectsOversizedBatch(t *testing.T) {
	svc, _ := newTestService(t)
	_, _, err := svc.Upload(context.Background(), uuid.New(), nil, uuid.New(), []files.UploadFileRequest{
		{OriginalName: "a.pdf", SizeBytes: files.MaxFileSizeBytes},
		{OriginalName: "b.pdf", SizeBytes: files.MaxFileSizeBytes},
		{OriginalName: "c.pdf", SizeBytes: files.MaxFileSizeBytes},
		{OriginalName: "d.pdf", SizeBytes: files.MaxFileSizeBytes},
		{OriginalName: "e.pdf", SizeBytes: files.MaxFileSizeBytes},
	})
	require.ErrorIs(t, err, files.ErrValidation)
}

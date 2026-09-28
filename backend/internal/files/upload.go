package files

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	miniogo "github.com/minio/minio-go/v7"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/clamav"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/minio"
)

var ErrValidation = errors.New("files: невалидный запрос")

// ProcessGate — то, что files ожидает от process (структурная инверсия зависимостей, без импорта
// пакета process — см. docs/architecture.md).
type ProcessGate interface {
	EnsureUploadable(ctx context.Context, tx pgx.Tx, objectID uuid.UUID, processID *uuid.UUID, actorID uuid.UUID) (uuid.UUID, error)
	UpdateScenario(ctx context.Context, tx pgx.Tx, processID uuid.UUID, scenario string) error
}

type UploadFileRequest struct {
	OriginalName string
	SizeBytes    int64
}

type PresignedUpload struct {
	FileID       uuid.UUID
	OriginalName string
	UploadURL    string
	UploadFields map[string]string
	ExpiresAt    time.Time
}

type Service struct {
	database *db.DB
	repo     *Repository
	minio    *minio.Client
	clamav   *clamav.Client
	process  ProcessGate
}

func NewService(database *db.DB, repo *Repository, minioClient *minio.Client, clamavClient *clamav.Client, process ProcessGate) *Service {
	return &Service{database: database, repo: repo, minio: minioClient, clamav: clamavClient, process: process}
}

const presignTTL = 30 * time.Minute

// Upload — backend-plan.md §8.1.1: лимиты 50/200 МБ по заявленным размерам, создать files(UPLOADING),
// выдать presigned POST с SetContentLengthRange и сроком 30 мин.
func (s *Service) Upload(ctx context.Context, objectID uuid.UUID, processID *uuid.UUID, actorID uuid.UUID, reqFiles []UploadFileRequest) (uuid.UUID, []PresignedUpload, error) {
	if len(reqFiles) == 0 {
		return uuid.Nil, nil, fmt.Errorf("%w: пустой список файлов", ErrValidation)
	}

	var totalSize int64
	for _, f := range reqFiles {
		if f.SizeBytes <= 0 || f.SizeBytes > MaxFileSizeBytes {
			return uuid.Nil, nil, fmt.Errorf("%w: файл %q превышает лимит 50 МБ либо имеет некорректный заявленный размер", ErrValidation, f.OriginalName)
		}
		totalSize += f.SizeBytes
	}
	if totalSize > MaxBatchSizeBytes {
		return uuid.Nil, nil, fmt.Errorf("%w: суммарный заявленный размер пакета превышает 200 МБ", ErrValidation)
	}

	var resultProcessID uuid.UUID
	var uploads []PresignedUpload

	err := s.database.WithTx(ctx, func(tx pgx.Tx) error {
		pid, err := s.process.EnsureUploadable(ctx, tx, objectID, processID, actorID)
		if err != nil {
			return err
		}
		resultProcessID = pid

		txRepo := NewRepository(tx)
		for _, f := range reqFiles {
			fileID := uuid.New()
			storageKey := fmt.Sprintf("%s/%s/%s", objectID, pid, fileID)

			if err := txRepo.CreateUploading(ctx, File{
				ID: fileID, ObjectID: objectID, ProcessID: pid, OriginalName: f.OriginalName,
				StorageKey: storageKey, SizeBytes: f.SizeBytes,
			}); err != nil {
				return err
			}

			url, fields, err := s.presignUpload(ctx, storageKey, f.SizeBytes)
			if err != nil {
				return err
			}

			uploads = append(uploads, PresignedUpload{
				FileID: fileID, OriginalName: f.OriginalName, UploadURL: url, UploadFields: fields,
				ExpiresAt: time.Now().Add(presignTTL),
			})
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, nil, err
	}
	return resultProcessID, uploads, nil
}

func (s *Service) presignUpload(ctx context.Context, storageKey string, sizeBytes int64) (string, map[string]string, error) {
	policy := miniogo.NewPostPolicy()
	if err := policy.SetBucket(s.minio.BucketName()); err != nil {
		return "", nil, fmt.Errorf("set policy bucket: %w", err)
	}
	if err := policy.SetKey(storageKey); err != nil {
		return "", nil, fmt.Errorf("set policy key: %w", err)
	}
	if err := policy.SetExpires(time.Now().UTC().Add(presignTTL)); err != nil {
		return "", nil, fmt.Errorf("set policy expiry: %w", err)
	}
	// SetContentLengthRange — иначе лимит по заявленному размеру не проверяется хранилищем
	// (backend-plan.md §2 "Проверить в перенесённом коде").
	if err := policy.SetContentLengthRange(1, sizeBytes); err != nil {
		return "", nil, fmt.Errorf("set policy content length range: %w", err)
	}

	u, formData, err := s.minio.Presigner().PresignedPostPolicy(ctx, policy)
	if err != nil {
		return "", nil, fmt.Errorf("presign upload: %w", err)
	}
	return u.String(), formData, nil
}

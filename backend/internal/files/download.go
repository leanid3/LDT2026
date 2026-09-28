package files

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
)

const downloadTTL = 15 * time.Minute

// DownloadURL — временная presigned-ссылка на GET файла из MinIO: интерфейс показывает документ рядом
// с подсветкой bbox из доказательства. Отдаём только принятые файлы: непроверенное (UPLOADING) и
// отклонённое (вирус, битый формат) скачивать из интерфейса не нужно и небезопасно.
func (s *Service) DownloadURL(ctx context.Context, fileID uuid.UUID) (string, time.Time, error) {
	f, err := s.repo.Get(ctx, fileID)
	if err != nil {
		return "", time.Time{}, err
	}
	if f.CheckStatus != CheckStatusAccepted {
		return "", time.Time{}, fmt.Errorf("%w: файл в статусе %s", ErrNotDownloadable, f.CheckStatus)
	}

	q := url.Values{}
	q.Set("response-content-disposition", fmt.Sprintf("inline; filename*=UTF-8''%s", url.PathEscape(f.OriginalName)))
	u, err := s.minio.Presigner().PresignedGetObject(ctx, s.minio.BucketName(), f.StorageKey, downloadTTL, q)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("presign get: %w", err)
	}
	return u.String(), time.Now().Add(downloadTTL), nil
}

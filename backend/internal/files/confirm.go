package files

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

type ConfirmedFile struct {
	FileID      uuid.UUID
	CheckStatus string
	CheckError  string
	PageCount   int
}

// Confirm — backend-plan.md §8.1.2: для каждого ещё не проверенного файла процесса, одним потоком из
// MinIO — sha256 + ClamAV INSTREAM, затем magic bytes, для PDF — pdfcpu validate + число страниц.
func (s *Service) Confirm(ctx context.Context, processID uuid.UUID) ([]ConfirmedFile, error) {
	fs, err := s.repo.ListByProcess(ctx, processID)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}

	results := make([]ConfirmedFile, 0, len(fs))
	for _, f := range fs {
		if f.CheckStatus != CheckStatusUploading && f.CheckStatus != CheckStatusUploaded {
			continue // уже проверен ранее — повторный confirm идемпотентен
		}
		results = append(results, s.confirmOne(ctx, processID, f))
	}

	// Пересчитать выбор редакции/статусы загрузки/сценарий с учётом новых ACCEPTED файлов
	// (без реестра — просто CLARIFICATION_REQUIRED по REGISTRY_MISSING, как и предписывает §8.2).
	if err := s.database.WithTx(ctx, func(tx pgx.Tx) error {
		txRepo := NewRepository(tx)
		allFiles, err := txRepo.ListByProcess(ctx, processID)
		if err != nil {
			return err
		}

		hasRegistry, err := hasRegistryEntries(ctx, tx, processID)
		if err != nil {
			return err
		}
		if !hasRegistry {
			for _, f := range allFiles {
				if f.CheckStatus == CheckStatusAccepted {
					if err := txRepo.UpdateSelection(ctx, f.ID, false, SelectionStatusClarificationRequired, "REGISTRY_MISSING"); err != nil {
						return err
					}
				}
			}
		} else if err := recomputeSelection(ctx, txRepo, allFiles); err != nil {
			return err
		}

		finalFiles, err := txRepo.ListByProcess(ctx, processID)
		if err != nil {
			return err
		}
		return refreshUploadStatusAndScenario(ctx, tx, txRepo, s.process, processID, finalFiles, nil)
	}); err != nil {
		return results, fmt.Errorf("recompute selection after confirm: %w", err)
	}

	return results, nil
}

func hasRegistryEntries(ctx context.Context, tx pgx.Tx, processID uuid.UUID) (bool, error) {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM registry_entries WHERE process_id = $1`, processID).Scan(&count); err != nil {
		return false, fmt.Errorf("count registry_entries: %w", err)
	}
	return count > 0, nil
}

func (s *Service) confirmOne(ctx context.Context, processID uuid.UUID, f File) ConfirmedFile {
	obj, err := s.minio.Raw().GetObject(ctx, s.minio.BucketName(), f.StorageKey, miniogo.GetObjectOptions{})
	if err != nil {
		return s.reject(ctx, f.ID, CheckStatusRejectedCorrupt, "не удалось прочитать объект из MinIO: "+err.Error())
	}
	defer func() { _ = obj.Close() }()

	hasher := sha256.New()
	var buf bytes.Buffer
	tee := io.TeeReader(obj, io.MultiWriter(hasher, &buf))

	scanResult, err := s.clamav.ScanReader(ctx, tee)
	if err != nil {
		return s.reject(ctx, f.ID, CheckStatusRejectedCorrupt, "антивирусная проверка недоступна: "+err.Error())
	}
	if scanResult.Infected {
		if _, err := s.minio.Raw().StatObject(ctx, s.minio.BucketName(), f.StorageKey, miniogo.StatObjectOptions{}); err == nil {
			_ = s.minio.Raw().RemoveObject(ctx, s.minio.BucketName(), f.StorageKey, miniogo.RemoveObjectOptions{})
		}
		return s.reject(ctx, f.ID, CheckStatusRejectedVirus, "обнаружен вирус: "+scanResult.Signature)
	}

	contentType, formatErr := detectFormat(buf.Bytes())
	if formatErr != nil {
		return s.reject(ctx, f.ID, CheckStatusRejectedFormat, formatErr.Error())
	}

	var pageCount int
	if contentType == contentTypePDF {
		n, err := countPDFPages(buf.Bytes())
		if err != nil {
			return s.reject(ctx, f.ID, CheckStatusRejectedCorrupt, "повреждённый PDF: "+err.Error())
		}
		pageCount = n
	}

	sha := hex.EncodeToString(hasher.Sum(nil))

	// Дубликат по sha256 в рамках процесса (backend-plan.md §8.1.3). UNIQUE(process_id, sha256)
	// не позволяет записать тот же хеш дважды — дубликату просто не проставляем sha256 (остаётся
	// NULL), он получает ACCEPTED, но не участвует в дальнейшей дедупликации по хешу.
	// TODO(M2): полноценно "вернуть существующий file_id" на уровне API-ответа confirm.
	if existing, err := s.repo.FindBySHA256InProcess(ctx, processID, sha); err == nil && existing.ID != f.ID {
		ct := contentType
		var pc *int
		if pageCount > 0 {
			pc = &pageCount
		}
		if err := s.repo.UpdateCheckResult(ctx, f.ID, nil, &ct, pc, CheckStatusAccepted, ptr("дубликат содержимого файла "+existing.ID.String())); err != nil {
			return s.reject(ctx, f.ID, CheckStatusRejectedCorrupt, "не удалось сохранить результат проверки: "+err.Error())
		}
		return ConfirmedFile{FileID: f.ID, CheckStatus: CheckStatusAccepted, PageCount: pageCount}
	}

	shaPtr, ctPtr := &sha, &contentType
	var pcPtr *int
	if pageCount > 0 {
		pcPtr = &pageCount
	}
	if err := s.repo.UpdateCheckResult(ctx, f.ID, shaPtr, ctPtr, pcPtr, CheckStatusAccepted, nil); err != nil {
		return s.reject(ctx, f.ID, CheckStatusRejectedCorrupt, "не удалось сохранить результат проверки: "+err.Error())
	}
	return ConfirmedFile{FileID: f.ID, CheckStatus: CheckStatusAccepted, PageCount: pageCount}
}

func (s *Service) reject(ctx context.Context, fileID uuid.UUID, status, reason string) ConfirmedFile {
	if err := s.repo.UpdateCheckResult(ctx, fileID, nil, nil, nil, status, &reason); err != nil {
		slog.Default().Error("failed to persist file rejection", "file_id", fileID, "error", err)
	}
	return ConfirmedFile{FileID: fileID, CheckStatus: status, CheckError: reason}
}

const (
	contentTypePDF  = "application/pdf"
	contentTypeDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	contentTypeXML  = "application/xml"
)

// detectFormat — magic bytes (backend-plan.md §8.1.2): %PDF- / zip с word/document.xml / <?xml.
func detectFormat(data []byte) (string, error) {
	switch {
	case bytes.HasPrefix(data, []byte("%PDF-")):
		return contentTypePDF, nil
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fmt.Errorf("повреждённый DOCX (некорректный zip): %w", err)
		}
		for _, zf := range zr.File {
			if zf.Name == "word/document.xml" {
				return contentTypeDOCX, nil
			}
		}
		return "", errors.New("архив не содержит word/document.xml — не DOCX")
	case bytes.HasPrefix(trimBOMAndSpace(data), []byte("<?xml")):
		return contentTypeXML, nil
	default:
		return "", errors.New("неизвестный формат файла (ожидается PDF, DOCX или XML)")
	}
}

// countPDFPages валидирует PDF и считает страницы через pdfcpu. На достаточно битых файлах pdfcpu
// иногда паникует вместо возврата ошибки (проверено эмпирически на обрезанном xref) — recover()
// превращает это в обычный REJECTED_CORRUPTED, а не в падение всего confirm-запроса (ТЗ 9.1:
// "битые файлы отклонять", а не крашить процесс; middleware.Recovery на HTTP-уровне тоже подстрахует,
// но здесь ошибка должна быть штатной, не паникой).
func countPDFPages(data []byte) (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pdfcpu panic: %v", r)
		}
	}()

	conf := model.NewDefaultConfiguration()
	if err := api.Validate(bytes.NewReader(data), conf); err != nil {
		return 0, err
	}
	return api.PageCount(bytes.NewReader(data), conf)
}

func ptr(s string) *string { return &s }

// utf8BOM — байтовая метка порядка следования (U+FEFF), иногда добавляется редакторами перед XML-декларацией.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func trimBOMAndSpace(data []byte) []byte {
	data = bytes.TrimPrefix(data, utf8BOM)
	return bytes.TrimLeft(data, " \t\r\n")
}

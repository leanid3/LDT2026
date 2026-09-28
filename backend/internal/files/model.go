package files

import (
	"time"

	"github.com/google/uuid"
)

// Статусы file_check_status — backend-plan.md §4.1.
const (
	CheckStatusUploading       = "UPLOADING"
	CheckStatusUploaded        = "UPLOADED"
	CheckStatusRejectedFormat  = "REJECTED_FORMAT"
	CheckStatusRejectedSize    = "REJECTED_SIZE"
	CheckStatusRejectedCorrupt = "REJECTED_CORRUPTED"
	CheckStatusRejectedVirus   = "REJECTED_VIRUS"
	CheckStatusAccepted        = "ACCEPTED"
)

const (
	MaxFileSizeBytes  = 50 * 1024 * 1024  // 50 МБ на файл (ТЗ 9.1)
	MaxBatchSizeBytes = 200 * 1024 * 1024 // 200 МБ на пакет (ТЗ 9.1)
)

type File struct {
	ID           uuid.UUID
	ObjectID     uuid.UUID
	ProcessID    uuid.UUID
	OriginalName string
	StorageKey   string
	SizeBytes    int64
	SHA256       *string
	ContentType  *string
	PageCount    *int

	CheckStatus string
	CheckError  *string

	DocStage        *string
	Discipline      *string
	DocumentCode    *string
	Revision        *string
	ApprovalStatus  *string
	ApprovalDate    *time.Time
	SheetPageRange  *string
	PredecessorID   *uuid.UUID
	SuccessorID     *uuid.UUID
	SignatureStatus *string

	IsCurrent       bool
	SelectionStatus *string
	SelectionReason *string

	UploadedAt time.Time
}

// RegistryRow — одна строка реестра (CSV/JSON/XLSX), backend-plan.md §8.2.
type RegistryRow struct {
	ObjectID        string
	FileName        string
	SHA256          string
	DocStage        string
	Discipline      string
	DocumentCode    string
	Revision        string
	ApprovalStatus  string
	ApprovalDate    string
	SheetPageRange  string
	PredecessorRef  string // sha256 или имя файла предшественника
	SuccessorRef    string
	SignatureStatus string
}

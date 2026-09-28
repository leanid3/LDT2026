// Package protocol — сборка и хранение протокола проверки (backend-plan.md §8.7). На этом этапе —
// только JSON-представление; экспорт в PDF/XML/DOCX через gotenberg — M4 (не в этом объёме).
package protocol

import (
	"time"

	"github.com/google/uuid"
)

// protocol_status — backend-plan.md §4.1.
const (
	StatusDraft                 = "DRAFT"
	StatusVerificationCompleted = "VERIFICATION_COMPLETED"
	StatusFinalized             = "PROTOCOL_FINALIZED"
)

// sync_status — backend-plan.md §4.1.
const (
	SyncStatusNotRequired = "NOT_REQUIRED"
	SyncStatusPending     = "PENDING_SYNC"
	SyncStatusSynced      = "SYNCED"
	SyncStatusFailed      = "SYNC_FAILED"
)

type Protocol struct {
	ID             uuid.UUID
	ProcessID      uuid.UUID
	ObjectID       uuid.UUID
	Version        int
	Status         string
	MatrixVersion  *string
	DatasetVersion *string
	ModelVersion   *string
	CreatedAt      time.Time
	FinalizedAt    *time.Time
	SyncStatus     string
}

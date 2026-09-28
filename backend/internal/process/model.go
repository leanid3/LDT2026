// Package process — процесс проверки: машина состояний, сценарий, статусы загрузки, запуск,
// финализация (backend-plan.md §4.2, §8.4, §8.8; диаграмма — docs/lifecycle.md#процесс-проверки).
package process

import (
	"time"

	"github.com/google/uuid"
)

// process_status — backend-plan.md §4.1.
const (
	StatusPending   = "PENDING"
	StatusParsing   = "PARSING"
	StatusReady     = "READY"
	StatusVerifying = "VERIFYING"
	StatusCompleted = "COMPLETED"
	StatusFinalized = "FINALIZED"
)

// scenario — backend-plan.md §4.1, §8.4.
const (
	ScenarioFull            = "FULL"
	ScenarioPDRDOnly        = "PD_RD_ONLY"
	ScenarioPDIDOnly        = "PD_ID_ONLY"
	ScenarioRDIDOnly        = "RD_ID_ONLY"
	ScenarioSingleOnly      = "SINGLE_ONLY"
	ScenarioPartiallyLoaded = "PARTIALLY_LOADED"
)

// doc_stage — backend-plan.md §4.1.
const (
	StagePD = "PD"
	StageRD = "RD"
	StageID = "ID"
)

// upload_status (по стадии) — backend-plan.md §5, таблица upload_status.
const (
	UploadStatusUploaded = "UPLOADED"
	UploadStatusPartial  = "PARTIAL"
	UploadStatusMissing  = "MISSING"
)

type Process struct {
	ID            uuid.UUID
	ObjectID      uuid.UUID
	Status        string
	Scenario      *string
	MatrixVersion *string
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
	FinalizedAt   *time.Time
	FinalizedBy   *uuid.UUID
}

type UploadStatusEntry struct {
	Stage  string
	Status string
}

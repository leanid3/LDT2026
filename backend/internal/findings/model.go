// Package findings — карточки доказательств и верификация (backend-plan.md §8.8).
package findings

import (
	"time"

	"github.com/google/uuid"
)

// finding_status — движок проверок (backend-plan.md §4.1).
const (
	StatusNegativeVerified      = "NEGATIVE_VERIFIED"
	StatusCandidate             = "CANDIDATE"
	StatusConfirmedViolation    = "CONFIRMED_VIOLATION"
	StatusMissingEvidence       = "MISSING_EVIDENCE"
	StatusNotApplicable         = "NOT_APPLICABLE"
	StatusNotComparable         = "NOT_COMPARABLE"
	StatusClarificationRequired = "CLARIFICATION_REQUIRED"
	StatusSuspicion             = "SUSPICION"
)

// inspector_status — решение человека (backend-plan.md §4.1). Только человек через Decide может
// поставить InspectorConfirmedViolation (backend/CLAUDE.md, правило 6).
const (
	InspectorPending               = "PENDING"
	InspectorConfirmedViolation    = "CONFIRMED_VIOLATION"
	InspectorNegativeVerified      = "NEGATIVE_VERIFIED"
	InspectorClarificationRequired = "CLARIFICATION_REQUIRED"
)

type Finding struct {
	ID              uuid.UUID
	CheckID         uuid.UUID
	ParentFindingID *uuid.UUID
	FindingStatus   string
	InspectorStatus string
	DecidedBy       *uuid.UUID
	DecidedAt       *time.Time
	ReasonCode      *string
	Comment         *string
	Version         int

	// Карточка для интерфейса: что проверялось, чем и почему (заполняется GetDetailed/ListByProcess).
	ParamCode      *string
	ParameterName  *string
	Unit           *string
	ReviewPriority *string
	ExpectedValue  *string
	ActualValue    *string
	Delta          *string
	Rationale      *string
	Evidence       []Evidence
}

// Evidence — фрагмент-доказательство одной стороны сравнения (evidence_fragments).
type Evidence struct {
	FileID         uuid.UUID
	OriginalName   string
	Stage          *string
	Page           *int
	BBox           []float64 // [x0,y0,x1,y1] в [0;1]; nil — геометрии нет
	Quote          *string
	ExtractedValue *string
	Role           string // expected | actual
}

package findings

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/audit"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/process"
)

var (
	ErrValidation        = errors.New("findings: невалидное решение")
	ErrInvalidTransition = errors.New("findings: верификация недопустима в текущем статусе процесса")
)

type Decision struct {
	Decision   string // CONFIRMED_VIOLATION | NEGATIVE_VERIFIED | CLARIFICATION_REQUIRED
	ReasonCode *string
	Comment    *string
}

type Service struct {
	database *db.DB
}

func NewService(database *db.DB) *Service {
	return &Service{database: database}
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Finding, error) {
	return NewRepository(s.database.Pool()).GetDetailed(ctx, id)
}

// HasPendingCandidates реализует process.FindingsGate структурно (docs/architecture.md, инверсия
// зависимостей — process не импортирует findings).
func (s *Service) HasPendingCandidates(ctx context.Context, tx pgx.Tx, processID uuid.UUID) (bool, error) {
	n, err := NewRepository(tx).CountPendingCandidates(ctx, processID)
	return n > 0, err
}

// Decide — backend-plan.md §8.8: атомарно обновить finding + audit_log; reason_code и comment
// обязательны для NEGATIVE_VERIFIED, comment обязателен для CONFIRMED_VIOLATION (backend-plan.md §7).
// Первое решение переводит READY -> VERIFYING; когда не остаётся CANDIDATE с inspector_status=PENDING
// -> VERIFYING -> COMPLETED (backend-plan.md §4.2).
func (s *Service) Decide(ctx context.Context, findingID, actorID uuid.UUID, d Decision) (Finding, error) {
	if err := validateDecision(d); err != nil {
		return Finding{}, err
	}

	var result Finding
	err := s.database.WithTx(ctx, func(tx pgx.Tx) error {
		repo := NewRepository(tx)

		f, err := repo.GetForUpdate(ctx, findingID)
		if err != nil {
			return err
		}

		processID, processStatus, err := repo.ProcessAndStatusForCheck(ctx, f.CheckID)
		if err != nil {
			return err
		}
		if processStatus != process.StatusReady && processStatus != process.StatusVerifying && processStatus != process.StatusCompleted {
			return fmt.Errorf("%w: процесс в статусе %s", ErrInvalidTransition, processStatus)
		}

		before := map[string]string{"inspector_status": f.InspectorStatus}
		if err := repo.UpdateDecision(ctx, findingID, d.Decision, actorID, d.ReasonCode, d.Comment); err != nil {
			return err
		}
		after := map[string]string{"inspector_status": d.Decision}

		if err := audit.Log(ctx, tx, audit.Entry{
			UserID: actorID, Action: "finding.decide", ObjectType: "finding", ObjectID: findingID.String(),
			Before: before, After: after,
			Details: map[string]any{"reason_code": d.ReasonCode, "comment": d.Comment},
		}); err != nil {
			return err
		}

		processRepo := process.NewRepository(tx)
		effectiveStatus := processStatus
		if effectiveStatus == process.StatusReady {
			if err := processRepo.UpdateStatus(ctx, processID, process.StatusVerifying); err != nil {
				return err
			}
			effectiveStatus = process.StatusVerifying
		}

		pending, err := repo.CountPendingCandidates(ctx, processID)
		if err != nil {
			return err
		}
		if pending == 0 && effectiveStatus == process.StatusVerifying {
			if err := processRepo.UpdateStatus(ctx, processID, process.StatusCompleted); err != nil {
				return err
			}
		}

		result, err = repo.GetDetailed(ctx, findingID)
		return err
	})
	return result, err
}

func validateDecision(d Decision) error {
	switch d.Decision {
	case InspectorConfirmedViolation:
		if d.Comment == nil || *d.Comment == "" {
			return fmt.Errorf("%w: для CONFIRMED_VIOLATION обязателен comment (backend-plan.md §7)", ErrValidation)
		}
	case InspectorNegativeVerified:
		if d.ReasonCode == nil || *d.ReasonCode == "" || d.Comment == nil || *d.Comment == "" {
			return fmt.Errorf("%w: для NEGATIVE_VERIFIED обязательны reason_code и comment (backend-plan.md §7)", ErrValidation)
		}
	case InspectorClarificationRequired:
		// без обязательных полей
	default:
		return fmt.Errorf("%w: недопустимое значение decision %q", ErrValidation, d.Decision)
	}
	return nil
}

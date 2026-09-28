package protocol

import (
	"context"

	"github.com/google/uuid"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/findings"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
)

type Assembled struct {
	Protocol Protocol
	Findings []findings.Finding
}

type Service struct {
	database *db.DB
}

func NewService(database *db.DB) *Service {
	return &Service{database: database}
}

// Get — актуальная версия протокола + все findings процесса (backend-plan.md §8.7). Полная разбивка
// на 5 таблиц и экспорт PDF/XML — M4, здесь только сводный JSON.
func (s *Service) Get(ctx context.Context, processID uuid.UUID) (Assembled, error) {
	p, err := NewRepository(s.database.Pool()).LatestByProcess(ctx, processID)
	if err != nil {
		return Assembled{}, err
	}

	fs, err := findings.NewRepository(s.database.Pool()).ListByProcess(ctx, processID)
	if err != nil {
		return Assembled{}, err
	}

	return Assembled{Protocol: p, Findings: fs}, nil
}

package rin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/findings"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/protocol"
)

// Payload — то, что уходит в ИАИС «РиН»: backend-plan.md §8.9 — только CONFIRMED_VIOLATION findings
// + версии протокола/Матрицы/модели (не весь протокол целиком).
type Payload struct {
	ProcessID      uuid.UUID          `json:"process_id"`
	ProtocolID     uuid.UUID          `json:"protocol_id"`
	ProtocolVer    int                `json:"protocol_version"`
	MatrixVersion  *string            `json:"matrix_version,omitempty"`
	DatasetVersion *string            `json:"dataset_version,omitempty"`
	ModelVersion   *string            `json:"model_version,omitempty"`
	Findings       []ConfirmedFinding `json:"findings"`
	Signature      string             `json:"signature,omitempty"`
}

type ConfirmedFinding struct {
	ID        uuid.UUID  `json:"id"`
	CheckID   uuid.UUID  `json:"check_id"`
	Comment   *string    `json:"comment,omitempty"`
	DecidedAt *time.Time `json:"decided_at,omitempty"`
}

// BuildPayload собирает данные для отправки: актуальная версия протокола + все findings с
// inspector_status=CONFIRMED_VIOLATION по процессу.
func BuildPayload(ctx context.Context, database *db.DB, processID uuid.UUID, signer Signer) (Payload, error) {
	p, err := protocol.NewRepository(database.Pool()).LatestByProcess(ctx, processID)
	if err != nil {
		return Payload{}, fmt.Errorf("load protocol: %w", err)
	}

	all, err := findings.NewRepository(database.Pool()).ListByProcess(ctx, processID)
	if err != nil {
		return Payload{}, fmt.Errorf("load findings: %w", err)
	}

	confirmed := make([]ConfirmedFinding, 0, len(all))
	for _, f := range all {
		if f.InspectorStatus == findings.InspectorConfirmedViolation {
			confirmed = append(confirmed, ConfirmedFinding{ID: f.ID, CheckID: f.CheckID, Comment: f.Comment, DecidedAt: f.DecidedAt})
		}
	}

	payload := Payload{
		ProcessID: processID, ProtocolID: p.ID, ProtocolVer: p.Version,
		MatrixVersion: p.MatrixVersion, DatasetVersion: p.DatasetVersion, ModelVersion: p.ModelVersion,
		Findings: confirmed,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Payload{}, fmt.Errorf("marshal payload for signing: %w", err)
	}
	sig, err := signer.Sign(body)
	if err != nil {
		return Payload{}, fmt.Errorf("sign payload: %w", err)
	}
	payload.Signature = sig

	return payload, nil
}

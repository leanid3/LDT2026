// Package audit пишет audit_log в той же транзакции, что бизнес-изменение (backend-plan.md §12.4,
// backend/CLAUDE.md: все изменяющие запросы + все решения + финализация/отмена — в аудит).
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Entry struct {
	UserID     uuid.UUID
	Action     string
	ObjectType string
	ObjectID   string
	Details    any
	Before     any
	After      any
	IP         string
	UserAgent  string
	RequestID  string
}

// Log — вызывается доменными сервисами внутри db.WithTx, рядом с самим бизнес-изменением.
func Log(ctx context.Context, tx pgx.Tx, e Entry) error {
	details, err := marshalOrNil(e.Details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}
	before, err := marshalOrNil(e.Before)
	if err != nil {
		return fmt.Errorf("marshal audit before: %w", err)
	}
	after, err := marshalOrNil(e.After)
	if err != nil {
		return fmt.Errorf("marshal audit after: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO audit_log (user_id, action, object_type, object_id, details, before, after, user_agent, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, nullableUUID(e.UserID), e.Action, e.ObjectType, e.ObjectID, details, before, after, e.UserAgent, e.RequestID)
	if err != nil {
		return fmt.Errorf("insert audit_log: %w", err)
	}
	return nil
}

func marshalOrNil(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

func nullableUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

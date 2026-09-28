// Package objects — объекты строительства (backend-plan.md §7 "Объекты и процессы").
package objects

import (
	"time"

	"github.com/google/uuid"
)

type Object struct {
	ID           uuid.UUID
	ExternalID   *string
	Name         string
	Address      *string
	Customer     *string
	Contractor   *string
	PermitNumber *string
	CreatedAt    time.Time
}

// Package auth — логин, JWT, роли (backend-plan.md §8.10, §12). bcrypt для паролей, JWT HS256 для
// токенов (12ч для демо), middleware для контекста запроса.
package auth

import "github.com/google/uuid"

// Роли — строго как в backend-plan.md §4.1, не изобретать новые.
const (
	RoleInspector  = "inspector"
	RoleSupervisor = "supervisor"
	RoleAdmin      = "admin"
	RoleMLEngineer = "ml_engineer"
)

type User struct {
	ID           uuid.UUID
	Login        string
	PasswordHash string
	FullName     string
	Role         string
	IsActive     bool
}

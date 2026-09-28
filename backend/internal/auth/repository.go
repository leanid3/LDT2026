package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("auth: пользователь не найден")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) FindByLogin(ctx context.Context, login string) (*User, error) {
	return r.scanOne(ctx, `
		SELECT id, login, password_hash, full_name, role, is_active FROM users WHERE login = $1
	`, login)
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*User, error) {
	return r.scanOne(ctx, `
		SELECT id, login, password_hash, full_name, role, is_active FROM users WHERE id = $1
	`, id)
}

func (r *Repository) scanOne(ctx context.Context, query string, arg any) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx, query, arg).Scan(&u.ID, &u.Login, &u.PasswordHash, &u.FullName, &u.Role, &u.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	return &u, nil
}

// Create — используется cmd/tools/seed-users; идемпотентно (ON CONFLICT DO NOTHING).
func (r *Repository) Create(ctx context.Context, u User) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO users (id, login, password_hash, full_name, role, is_active)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (login) DO NOTHING
	`, u.ID, u.Login, u.PasswordHash, u.FullName, u.Role, u.IsActive)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

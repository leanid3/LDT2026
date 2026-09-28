package objects

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("objects: объект не найден")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, o Object) (Object, error) {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO objects (id, external_id, name, address, customer, contractor, permit_number)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at
	`, o.ID, o.ExternalID, o.Name, o.Address, o.Customer, o.Contractor, o.PermitNumber).Scan(&o.CreatedAt)
	if err != nil {
		return Object{}, fmt.Errorf("insert object: %w", err)
	}
	return o, nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (Object, error) {
	var o Object
	err := r.pool.QueryRow(ctx, `
		SELECT id, external_id, name, address, customer, contractor, permit_number, created_at
		FROM objects WHERE id = $1
	`, id).Scan(&o.ID, &o.ExternalID, &o.Name, &o.Address, &o.Customer, &o.Contractor, &o.PermitNumber, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Object{}, ErrNotFound
	}
	if err != nil {
		return Object{}, fmt.Errorf("query object: %w", err)
	}
	return o, nil
}

func (r *Repository) List(ctx context.Context) ([]Object, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, external_id, name, address, customer, contractor, permit_number, created_at
		FROM objects ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query objects: %w", err)
	}
	defer rows.Close()

	var out []Object
	for rows.Next() {
		var o Object
		if err := rows.Scan(&o.ID, &o.ExternalID, &o.Name, &o.Address, &o.Customer, &o.Contractor, &o.PermitNumber, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan object: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

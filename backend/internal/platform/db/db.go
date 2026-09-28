// Package db оборачивает pgx/v5 pgxpool: подключение, health-check и WithTx
// (unit of work, чтобы outbox-запись и бизнес-изменение шли в одной транзакции — backend-plan.md §2).
package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier — общий поднабор *pgxpool.Pool и pgx.Tx: доменные репозитории пишут методы через этот
// интерфейс и работают одинаково что вне транзакции (обычные чтения), что внутри WithTx (шаг
// unit-of-work вместе с другими изменениями/outbox-записью).
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Config struct {
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	MaxConnections  int32
	MinConnections  int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

func (c Config) dsn() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?pool_max_conns=%d&pool_min_conns=%d",
		c.User, c.Password, c.Host, c.Port, c.Database, c.MaxConnections, c.MinConnections)
}

type DB struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// New подключается к Postgres и проверяет соединение пингом.
func New(ctx context.Context, cfg Config, log *slog.Logger) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.dsn())
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConnections
	poolCfg.MinConns = cfg.MinConnections
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	log.Info("postgres connected", "host", cfg.Host, "port", cfg.Port, "database", cfg.Database)
	return &DB{pool: pool, log: log}, nil
}

func (d *DB) Close() {
	d.pool.Close()
}

func (d *DB) Pool() *pgxpool.Pool {
	return d.pool
}

// HealthCheck используется в /readyz.
func (d *DB) HealthCheck(ctx context.Context) error {
	return d.pool.Ping(ctx)
}

// WithTx — unit of work: коммитит при успехе, откатывает при ошибке или панике.
func (d *DB) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) (err error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = errors.Join(err, fmt.Errorf("rollback tx: %w", rbErr))
			}
			return
		}
		err = tx.Commit(ctx)
	}()

	err = fn(tx)
	return err
}

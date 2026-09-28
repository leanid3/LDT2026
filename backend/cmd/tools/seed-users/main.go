// cmd/tools/seed-users — тестовые пользователи (по одному на роль), backend-plan.md §8.10.
// Идемпотентно: повторный запуск не создаёт дублей (ON CONFLICT (login) DO NOTHING).
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/google/uuid"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/auth"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/config"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/logging"
)

// demoPassword — единый пароль для всех тестовых пользователей на локальном/демо-стенде.
// НЕ для использования вне dev-окружения (backend/CLAUDE.md, правило 10 — секреты не в репозитории;
// это заведомо публичный dev-пароль, не секрет).
const demoPassword = "demo-password-123"

var demoUsers = []struct {
	login    string
	fullName string
	role     string
}{
	{"inspector1", "Инспектор Демо", auth.RoleInspector},
	{"supervisor1", "Супервизор Демо", auth.RoleSupervisor},
	{"admin1", "Администратор Демо", auth.RoleAdmin},
	{"ml1", "ML-инженер Демо", auth.RoleMLEngineer},
}

func main() {
	log := logging.New("info", "seed-users")

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	database, err := db.New(ctx, db.Config{
		Host: cfg.Database.Host, Port: cfg.Database.Port, User: cfg.Database.User,
		Password: cfg.Database.Password, Database: cfg.Database.Database,
		MaxConnections: cfg.Database.MaxConnections, MinConnections: cfg.Database.MinConnections,
		MaxConnLifetime: cfg.Database.MaxConnLifetime, MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
	}, log)
	if err != nil {
		log.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	hash, err := auth.HashPassword(demoPassword)
	if err != nil {
		log.Error("failed to hash demo password", "error", err)
		os.Exit(1)
	}

	repo := auth.NewRepository(database.Pool())
	for _, u := range demoUsers {
		if err := repo.Create(ctx, auth.User{
			ID: uuid.New(), Login: u.login, PasswordHash: hash, FullName: u.fullName, Role: u.role, IsActive: true,
		}); err != nil {
			log.Error("failed to create user", "login", u.login, "error", err)
			os.Exit(1)
		}
		log.Info("user ready", "login", u.login, "role", u.role, slog.String("password", demoPassword))
	}

	log.Info("seed-users done", "count", len(demoUsers))
}

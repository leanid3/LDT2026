// cmd/api — HTTP API сервис «Инспектор ИИ».
//
// На M0 (backend-plan.md, веха M0) сервис отдаёт только health/readiness/metrics и черновик
// contracts/openapi.yaml через Swagger UI. Доменные хендлеры (auth, objects, files, process, ...)
// появятся начиная с M1-M2 вместе с полным contracts/openapi.yaml + `make gen`.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/gin-gonic/gin"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/config"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/health"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/httpx"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/httpx/middleware"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/logging"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/metrics"
)

const openAPIPathEnv = "OPENAPI_PATH"

func main() {
	log := logging.New("info", "api")

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	log = logging.New(cfg.Logger.Level, "api")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	database, err := db.New(ctx, db.Config{
		Host:            cfg.Database.Host,
		Port:            cfg.Database.Port,
		User:            cfg.Database.User,
		Password:        cfg.Database.Password,
		Database:        cfg.Database.Database,
		MaxConnections:  cfg.Database.MaxConnections,
		MinConnections:  cfg.Database.MinConnections,
		MaxConnLifetime: cfg.Database.MaxConnLifetime,
		MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
	}, log)
	if err != nil {
		log.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	m := metrics.New()
	startMetricsServer(cfg.Metrics, m, log)

	server := httpx.New(log,
		httpx.Port(cfg.Server.Port),
		httpx.ReadTimeout(cfg.Server.ReadTimeout),
		httpx.WriteTimeout(cfg.Server.WriteTimeout),
		httpx.ShutdownTimeout(cfg.Server.ShutdownTimeout),
	)

	engine := server.Engine()
	engine.Use(middleware.RequestID(), middleware.Recovery(log), middleware.Logger(log), m.Middleware())

	health.Register(engine, map[string]health.Checker{
		"postgres": database.HealthCheck,
	})

	registerSwagger(engine, log)

	server.Start()
	log.Info("api service started", "port", cfg.Server.Port)

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case err := <-server.Notify():
		if err != nil {
			log.Error("http server error", "error", err)
		}
	}

	if err := server.Shutdown(); err != nil {
		log.Error("shutdown error", "error", err)
	}
}

func startMetricsServer(cfg config.MetricsConfig, m *metrics.Metrics, log *slog.Logger) {
	if !cfg.Enabled {
		return
	}
	mux := http.NewServeMux()
	mux.Handle(cfg.Path, m.Handler())

	go func() {
		addr := fmt.Sprintf(":%d", cfg.Port)
		log.Info("starting metrics server", "addr", addr, "path", cfg.Path)
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			log.Error("metrics server failed", "error", err)
		}
	}()
}

// registerSwagger отдаёт черновик contracts/openapi.yaml статикой и простую Swagger UI страницу
// (backend-plan.md §2: swaggo убран, spec-first OpenAPI 3.0).
func registerSwagger(engine *gin.Engine, log *slog.Logger) {
	openapiPath := os.Getenv(openAPIPathEnv)
	if openapiPath == "" {
		openapiPath = filepath.Join("..", "contracts", "openapi.yaml")
	}

	engine.GET("/contracts/openapi.yaml", func(c *gin.Context) {
		if _, err := os.Stat(openapiPath); err != nil {
			c.String(http.StatusNotFound, "openapi.yaml not found: %v", err)
			return
		}
		c.File(openapiPath)
	})
	engine.GET("/swagger", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerUIPage))
	})

	log.Info("swagger UI registered", "openapi_path", openapiPath)
}

const swaggerUIPage = `<!DOCTYPE html>
<html>
<head>
  <title>Инспектор ИИ — API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = () => SwaggerUIBundle({
      url: "/contracts/openapi.yaml",
      dom_id: "#swagger-ui",
    });
  </script>
</body>
</html>`

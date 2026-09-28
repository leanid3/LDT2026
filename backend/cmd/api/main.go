// cmd/api — HTTP API сервис «Инспектор ИИ» (backend-plan.md §7). Health/readiness/metrics — с M0;
// доменные хендлеры (auth, objects, documents, processes, findings, protocol) — с этого захода
// (contracts/openapi.yaml v0.2.0), до границы, где начинается работа Python-воркеров
// (docs/architecture.md).
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

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/auth"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/config"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/files"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/findings"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/matrix"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/objects"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/clamav"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/health"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/httpx"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/httpx/middleware"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/logging"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/metrics"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/minio"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/process"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/protocol"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/transport/http/api"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/transport/http/gen"
)

const (
	openAPIPathEnv = "OPENAPI_PATH"
	apiBaseURL     = "/api/v1"
)

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

	minioClient, err := minio.New(ctx, minio.Config{
		Endpoint: cfg.Minio.Endpoint, AccessKey: cfg.Minio.AccessKey, SecretKey: cfg.Minio.SecretKey,
		Bucket: cfg.Minio.Bucket, UseSSL: cfg.Minio.UseSSL, Region: cfg.Minio.Region, Timeout: cfg.Minio.Timeout,
		PublicEndpoint: cfg.Minio.PublicEndpoint, PublicUseSSL: cfg.Minio.PublicUseSSL,
	}, log)
	if err != nil {
		log.Error("failed to connect to minio", "error", err)
		os.Exit(1)
	}

	clamavClient := clamav.New(clamav.Config{Address: cfg.ClamAV.Address, Timeout: cfg.ClamAV.Timeout})

	apiServer := wireServer(database, minioClient, clamavClient, cfg)

	m := metrics.New()
	startMetricsServer(cfg.Metrics, m, log)

	server := httpx.New(log,
		httpx.Port(cfg.Server.Port),
		httpx.ReadTimeout(cfg.Server.ReadTimeout),
		httpx.WriteTimeout(cfg.Server.WriteTimeout),
		httpx.ShutdownTimeout(cfg.Server.ShutdownTimeout),
	)

	engine := server.Engine()
	jwtIssuer := auth.NewIssuer(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL)
	engine.Use(middleware.RequestID(), middleware.Recovery(log), middleware.Logger(log), middleware.CORS(cfg.Server.CORSOrigins), m.Middleware(), auth.AttachClaims(jwtIssuer))

	health.Register(engine, map[string]health.Checker{
		"postgres": database.HealthCheck,
		"minio":    minioClient.HealthCheck,
		"clamav":   clamavClient.HealthCheck,
	})

	registerSwagger(engine, log)
	gen.RegisterHandlersWithOptions(engine, apiServer, gen.GinServerOptions{BaseURL: apiBaseURL})

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

// wireServer собирает граф зависимостей доменных пакетов (docs/architecture.md — узкие интерфейсы,
// определённые внутри process/files, сводятся друг с другом только здесь и в internal/transport/http/api).
func wireServer(database *db.DB, minioClient *minio.Client, clamavClient *clamav.Client, cfg *config.Config) *api.Server {
	authRepo := auth.NewRepository(database.Pool())
	jwtIssuer := auth.NewIssuer(cfg.Auth.JWTSecret, cfg.Auth.TokenTTL)
	authService := auth.NewService(authRepo, jwtIssuer)
	authHandler := auth.NewHandler(authService, authRepo)

	objectsRepo := objects.NewRepository(database.Pool())
	objectsHandler := objects.NewHandler(objectsRepo)

	filesRepo := files.NewRepository(database.Pool())
	findingsService := findings.NewService(database)
	fileLister := api.NewFileLister(filesRepo)
	processService := process.NewService(database, fileLister, findingsService)
	filesService := files.NewService(database, filesRepo, minioClient, clamavClient, processService)
	filesHandler := files.NewHandler(filesService)

	findingsHandler := findings.NewHandler(findingsService)

	protocolService := protocol.NewService(database)
	protocolHandler := protocol.NewHandler(protocolService)

	return api.NewServer(authHandler, objectsHandler, filesHandler, findingsHandler, protocolHandler, filesRepo, protocolService, processService,
		objectsRepo, matrix.NewRepository(database.Pool()))
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
      persistAuthorization: true, // токен из «Authorize» переживает перезагрузку страницы
      tryItOutEnabled: true,
    });
  </script>
</body>
</html>`

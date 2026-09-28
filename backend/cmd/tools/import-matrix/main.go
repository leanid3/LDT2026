// cmd/tools/import-matrix — импорт листа МАТРИЦА (132 параметра) из docs/source/*.xlsx в таблицу params
// (backend-plan.md §5, §11 M2) и экспорт каталога для Python-воркеров в contracts/matrix.json.
//
//	go run ./cmd/tools/import-matrix                       # xlsx -> БД + contracts/matrix.json
//	go run ./cmd/tools/import-matrix --no-db               # только экспорт JSON (БД не нужна)
//
// Идемпотентно: повторный запуск обновляет параметры по code.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/config"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine/rules"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/matrix"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/logging"
)

func main() {
	file := flag.String("file", "../docs/source/Матрица_параметров_редакция1.1.xlsx", "путь к xlsx Матрицы")
	rulesDir := flag.String("rules", "rules", "каталог правил (определяет data_type параметров)")
	version := flag.String("version", "1.1", "версия Матрицы (params.matrix_version)")
	out := flag.String("out", "../contracts/matrix.json", "куда экспортировать каталог для воркеров ('' — не экспортировать)")
	noDB := flag.Bool("no-db", false, "не писать в БД, только экспорт")
	flag.Parse()

	log := logging.New("info", "import-matrix")
	fail := func(msg string, err error) {
		log.Error(msg, "error", err)
		os.Exit(1)
	}

	params, err := matrix.ReadXLSX(*file)
	if err != nil {
		fail("read matrix", err)
	}
	ruleSet, err := rules.LoadDir(*rulesDir)
	if err != nil {
		fail("load rules", err)
	}
	matrix.Enrich(params, ruleSet)
	log.Info("matrix parsed", "params", len(params), "with_rules", len(ruleSet), "version", *version)

	if !*noDB {
		cfg, err := config.Load("config.yaml")
		if err != nil {
			fail("load config", err)
		}
		ctx := context.Background()
		database, err := db.New(ctx, db.Config{
			Host: cfg.Database.Host, Port: cfg.Database.Port, User: cfg.Database.User,
			Password: cfg.Database.Password, Database: cfg.Database.Database,
			MaxConnections: cfg.Database.MaxConnections, MinConnections: cfg.Database.MinConnections,
			MaxConnLifetime: cfg.Database.MaxConnLifetime, MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
		}, log)
		if err != nil {
			fail("connect to postgres", err)
		}
		defer database.Close()
		if err := matrix.Upsert(ctx, database, params, *version); err != nil {
			fail("upsert params", err)
		}
		log.Info("params upserted", "count", len(params))
	}

	if *out != "" {
		doc := map[string]any{"matrix_version": *version, "params": params}
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			fail("marshal matrix.json", err)
		}
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			fail("mkdir", err)
		}
		if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
			fail("write matrix.json", err)
		}
		log.Info("matrix.json written", "path", *out)
	}
}

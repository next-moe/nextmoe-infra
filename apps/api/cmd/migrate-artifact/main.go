package main

import (
	"log/slog"
	"os"

	"api/internal/infrastructure/database"
	artifactModel "api/internal/platform/artifact/model"
	"api/pkg/config"
	"api/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger.Init(cfg.Server.Env)
	slog.Info("connecting to artifacts database", "dbname", cfg.ArtifactsDatabase.DBName)

	db, err := database.NewPostgresDB(cfg.ArtifactsDatabase)
	if err != nil {
		slog.Error("failed to connect to database", "error", err,
			"hint", "create the database first, e.g. `createdb kun_artifacts`")
		os.Exit(1)
	}
	defer db.Close()

	slog.Info("running artifact migrations...")

	if err := db.AutoMigrate(artifactModel.All()...); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	slog.Info("artifact migration completed successfully")
}

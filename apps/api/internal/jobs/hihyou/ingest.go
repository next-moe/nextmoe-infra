package hihyou

import (
	"fmt"

	"api/internal/infrastructure/database"
	"api/pkg/config"

	"gorm.io/gorm"
)

type Opts struct {
	Apply       bool
	Concurrency int // parallel picture fetch/upload; the database writes stay serial
}

type stats struct {
	created      int
	updated      int
	unchanged    int
	truncated    int
	droppedEmpty int
	imagesUp     int
	imagesFail   int
	wouldCreate  int
	wouldUpdate  int
	wouldImages  int
}

func openNewsDB(cfg *config.Config) (*gorm.DB, func(), error) {
	conn, err := database.NewPostgresDB(cfg.NewsDatabase)
	if err != nil {
		return nil, nil, fmt.Errorf("news db: %w", err)
	}
	return conn.DB(), func() { _ = conn.Close() }, nil
}

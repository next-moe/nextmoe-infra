package main

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"api/internal/infrastructure/database"
	"api/pkg/config"

	aimigrate "api/internal/platform/ai/migrate"
	catalogmigrate "api/internal/platform/catalog/migrate"
	catalogseed "api/internal/platform/catalog/seed"
	chatmigrate "api/internal/platform/chat/migrate"
	communitymigrate "api/internal/platform/community/migrate"
	newsmigrate "api/internal/platform/news/migrate"
	telemetrymigrate "api/internal/platform/telemetry/migrate"
	trustmigrate "api/internal/platform/trust/migrate"

	"gorm.io/gorm"
)

// Each domain service owns its own database and its own migrate package. This
// binary is only the entry point: open the right connection, call that package.
type domain struct {
	db   func(*config.Config) config.DatabaseConfig
	run  func(*gorm.DB) error
	seed func(*gorm.DB) error // optional
}

var domains = map[string]domain{
	// The catalog target carried a SECOND family until wave 161. Since
	// wiki-retirement W5 it also AutoMigrated the galgame (wiki) models against
	// cfg.GalgameDatabase, which is how those 27 tables got (re)created on every
	// single deploy. That half is gone, and the deletion is load-bearing rather
	// than tidy-up: with it still in place, the N5 window would DROP the family
	// and the very next deploy would silently recreate all 27 tables as empty
	// shells — leaving the registry looking healthy while every consumer read
	// nothing. cfg.GalgameDatabase is consequently no longer opened here.
	//
	// Seeds run on every migrate (idempotent upserts): registry vocabularies are
	// data, and this is their single write path outside manual curation.
	"catalog": {
		db:   func(c *config.Config) config.DatabaseConfig { return c.CatalogDatabase },
		run:  catalogmigrate.Run,
		seed: catalogseed.Run,
	},
	"community": {
		db:  func(c *config.Config) config.DatabaseConfig { return c.CommunityDatabase },
		run: communitymigrate.Run,
	},
	"chat": {
		db:  func(c *config.Config) config.DatabaseConfig { return c.ChatDatabase },
		run: chatmigrate.Run,
	},
	"trust": {
		db:  func(c *config.Config) config.DatabaseConfig { return c.TrustDatabase },
		run: trustmigrate.Run,
	},
	"ai": {
		db:  func(c *config.Config) config.DatabaseConfig { return c.AIDatabase },
		run: aimigrate.Run,
	},
	"telemetry": {
		db:  func(c *config.Config) config.DatabaseConfig { return c.TelemetryDatabase },
		run: telemetrymigrate.Run,
	},
	"news": {
		db:  func(c *config.Config) config.DatabaseConfig { return c.NewsDatabase },
		run: newsmigrate.Run,
	},
}

func domainNames() string {
	names := make([]string, 0, len(domains))
	for name := range domains {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, " | ")
}

func runDomain(name string, cfg *config.Config) error {
	d, ok := domains[name]
	if !ok {
		return fmt.Errorf("unknown migration target %q (want one of: %s)", name, domainNames())
	}

	dbCfg := d.db(cfg)
	slog.Info("connecting to database", "target", name, "dbname", dbCfg.DBName)
	db, err := database.NewPostgresDB(dbCfg)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", dbCfg.DBName, err)
	}
	defer db.Close()

	slog.Info("running migrations", "target", name)
	if err := d.run(db.DB()); err != nil {
		return fmt.Errorf("migrate %s: %w", name, err)
	}

	if d.seed != nil {
		slog.Info("seeding registries", "target", name)
		if err := d.seed(db.DB()); err != nil {
			return fmt.Errorf("seed %s: %w", name, err)
		}
	}

	slog.Info("migration completed successfully", "target", name)
	return nil
}

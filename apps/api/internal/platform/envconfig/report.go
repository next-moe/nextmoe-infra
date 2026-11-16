package envconfig

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"time"

	"api/internal/platform/settings/keys"
	"api/pkg/config"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Report records what this process resolved at startup, so the console can
// show every variable of every service — including the ones left on their
// code default, which nothing else would ever reveal.
func Report(ctx context.Context, db *gorm.DB, service string) {
	if service == "" || service == config.ScopeTools {
		return
	}
	entries, err := json.Marshal(config.Snapshot())
	if err != nil {
		slog.Warn("config snapshot: encode failed", "err", err)
		return
	}
	unread, err := json.Marshal(nonNil(config.UnreadEnv(settingsFloors())))
	if err != nil {
		slog.Warn("config snapshot: encode failed", "err", err)
		return
	}
	host, _ := os.Hostname()
	row := BootSnapshot{
		Service:     service,
		Hostname:    host,
		StartedAt:   time.Now(),
		StoreLoaded: config.StoreKey() != nil,
		Entries:     entries,
		Unread:      unread,
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
		slog.Warn("config snapshot: write failed; the console shows this service's previous start", "service", service, "err", err)
	}
}

func settingsFloors() map[string]bool {
	out := map[string]bool{}
	for _, e := range keys.Live().Entries() {
		if env := e.Meta().EnvVar; env != "" {
			out[env] = true
		}
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

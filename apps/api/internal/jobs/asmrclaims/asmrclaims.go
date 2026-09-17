package asmrclaims

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"api/internal/infrastructure/database"
	"api/pkg/config"

	"gorm.io/gorm"
)

const (
	mediumASMR = int16(5)
	siteKungal = "kungal"
)

type Opts struct {
	DSN   string
	Apply bool
	Limit int
}

type candidate struct {
	ID            int64  `gorm:"column:id"`
	ProductWorkID int64  `gorm:"column:product_work_id"`
	RivalID       *int64 `gorm:"column:rival_id"`
}

func Run(ctx context.Context, cfg *config.Config, opts Opts) (map[string]any, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess — pass the rehearsal copy locally, the live catalog only in the production run")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}

	cands, err := loadCandidates(ctx, db, opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("load candidates: %w", err)
	}
	slog.Info("asmr-self-claim candidates", "candidates", len(cands), "apply", opts.Apply, "limit", opts.Limit)

	var toClear []candidate
	skippedNoRival := 0
	for _, c := range cands {
		if c.RivalID == nil {
			skippedNoRival++
			slog.Info("asmr-self-claim",
				"id", c.ID, "product_work_id", c.ProductWorkID, "rival_id", 0, "clearable", false)
			continue
		}
		toClear = append(toClear, c)
	}

	for _, c := range toClear {
		slog.Info("asmr-self-claim",
			"id", c.ID, "product_work_id", c.ProductWorkID, "rival_id", *c.RivalID, "clearable", true)
	}

	cleared := int64(0)
	if opts.Apply && len(toClear) > 0 {
		ids := candidateIDs(toClear)
		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// updated_at moves because the public changes feed reads
			// (updated_at, id); survivorship.go's unclaim sets it for the same
			// reason, and without it these rows never reach a consumer.
			res := tx.Exec(
				`UPDATE catalog_work SET site = NULL, product_work_id = NULL, updated_at = now() WHERE id IN ?`,
				ids,
			)
			cleared = res.RowsAffected
			return res.Error
		}); err != nil {
			return nil, fmt.Errorf("clear claims: %w", err)
		}
		slog.Info(undoSQL(ids))
	}

	sum := map[string]any{
		"candidates":       len(cands),
		"cleared":          int(cleared),
		"skipped_no_rival": skippedNoRival,
		"apply":            opts.Apply,
	}
	slog.Info("asmr-self-claim done", "summary", sum)
	return sum, nil
}

func loadCandidates(ctx context.Context, db *gorm.DB, limit int) ([]candidate, error) {
	q := `
		SELECT c.id, c.product_work_id,
		       (SELECT r.id
		          FROM catalog_work r
		         WHERE r.deleted_at IS NULL
		           AND r.site = ?
		           AND r.product_work_id = c.product_work_id
		           AND r.id <> c.id
		         ORDER BY r.id
		         LIMIT 1) AS rival_id
		  FROM catalog_work c
		 WHERE c.deleted_at IS NULL
		   AND c.medium_id = ?
		   AND c.site = ?
		   AND c.product_work_id = c.id
		 ORDER BY c.id`
	args := []any{siteKungal, mediumASMR, siteKungal}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	var out []candidate
	if err := db.WithContext(ctx).Raw(q, args...).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func candidateIDs(cands []candidate) []int64 {
	ids := make([]int64, len(cands))
	for i, c := range cands {
		ids[i] = c.ID
	}
	return ids
}

func undoSQL(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return "UPDATE catalog_work SET site = 'kungal', product_work_id = id WHERE id IN (" +
		strings.Join(parts, ", ") + ");"
}

package repository

import (
	"context"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type RedirectRepository struct {
	db *gorm.DB
}

func NewRedirectRepository(db *gorm.DB) *RedirectRepository {
	return &RedirectRepository{db: db}
}

func (r *RedirectRepository) Get(ctx context.Context, entityType int16, oldID int64) (*model.CatalogRedirect, error) {
	return getRedirect(r.db.WithContext(ctx), entityType, oldID)
}

func (r *RedirectRepository) GetBatch(ctx context.Context, entityType int16, ids []int64) (map[int64]int64, error) {
	var rows []model.CatalogRedirect
	if err := r.db.WithContext(ctx).
		Where("entity_type = ? AND old_id IN ? AND revoked_at IS NULL", entityType, ids).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]int64, len(rows))
	for _, row := range rows {
		out[row.OldID] = row.CurrentID
	}
	return out, nil
}

type RedirectCursor struct {
	MergedAt   time.Time
	EntityType int16
	OldID      int64
}

// A row-value comparison against a NULL merged_at is NULL, not true, so a
// redirect with no recorded merge time was invisible on every page of the feed
// and no mirror could ever learn about that merge. redirectOrdSQL folds those
// rows onto the epoch, which is also what the feed publishes for them.
const redirectOrdSQL = "COALESCE(merged_at, to_timestamp(0))"

func (r *RedirectRepository) Since(ctx context.Context, entityType *int16, cursor RedirectCursor, limit int) ([]model.CatalogRedirect, error) {
	q := r.db.WithContext(ctx).
		Where("("+redirectOrdSQL+", entity_type, old_id) > (?, ?, ?)", cursor.MergedAt, cursor.EntityType, cursor.OldID).
		Order(redirectOrdSQL + ", entity_type, old_id").
		Limit(limit)
	if entityType != nil {
		q = q.Where("entity_type = ?", *entityType)
	}
	var rows []model.CatalogRedirect
	err := q.Find(&rows).Error
	return rows, err
}

func (r *RedirectRepository) Count(ctx context.Context, entityType *int16) (int64, error) {
	q := r.db.WithContext(ctx).Model(&model.CatalogRedirect{})
	if entityType != nil {
		q = q.Where("entity_type = ?", *entityType)
	}
	var n int64
	err := q.Count(&n).Error
	return n, err
}

func getRedirect(db *gorm.DB, entityType int16, oldID int64) (*model.CatalogRedirect, error) {
	var row model.CatalogRedirect
	err := db.Where("entity_type = ? AND old_id = ? AND revoked_at IS NULL", entityType, oldID).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// The feed is ordered by merged_at, so a redirect whose current_id changes
// without merged_at moving is invisible to every mirror already past it: an
// older merge into the source kept pointing at the retired id downstream.
// Flattening and repointing therefore bump merged_at, which the feed documents
// as the time the redirect last changed.
func FlattenRedirectsTo(tx *gorm.DB, entityType int16, sourceID, targetID int64) error {
	return tx.Model(&model.CatalogRedirect{}).
		Where("entity_type = ? AND current_id = ? AND revoked_at IS NULL", entityType, sourceID).
		Updates(map[string]any{"current_id": targetID, "merged_at": time.Now()}).Error
}

func InsertRedirect(tx *gorm.DB, entityType int16, oldID, currentID int64, mergedBy *int64, reason string) error {
	return tx.Exec(`INSERT INTO catalog_redirect (entity_type, old_id, current_id, merged_at, merged_by, reason)
		VALUES (?, ?, ?, now(), ?, ?)
		ON CONFLICT (entity_type, old_id) DO UPDATE
		SET current_id = EXCLUDED.current_id, merged_at = EXCLUDED.merged_at, merged_by = EXCLUDED.merged_by,
		    reason = EXCLUDED.reason, revoked_at = NULL`,
		entityType, oldID, currentID, mergedBy, reason).Error
}

func RepointRedirect(tx *gorm.DB, entityType int16, oldID, newCurrentID int64, reason string) error {
	return tx.Model(&model.CatalogRedirect{}).
		Where("entity_type = ? AND old_id = ? AND revoked_at IS NULL", entityType, oldID).
		Updates(map[string]any{"current_id": newCurrentID, "reason": reason, "merged_at": time.Now()}).Error
}

// RevokeRedirect hands oldID back: the row stays so the feed can tell mirrors
// the id is live again (current_id = old_id, revoked), and every reader that
// resolves ids skips it.
func RevokeRedirect(tx *gorm.DB, entityType int16, oldID int64, reason string) error {
	return tx.Exec(`UPDATE catalog_redirect SET current_id = old_id, revoked_at = now(), merged_at = now(),
		    reason = CASE WHEN reason = '' THEN ? ELSE reason || '; ' || ? END
		WHERE entity_type = ? AND old_id = ? AND revoked_at IS NULL`,
		reason, reason, entityType, oldID).Error
}

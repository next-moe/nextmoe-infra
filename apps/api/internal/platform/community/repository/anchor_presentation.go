package repository

import (
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

type AnchorPresentationWrite struct {
	AnchorKind   int16
	AnchorID     string
	Title        string
	URL          string
	WorkID       *int64
	CoverHash    *string
	ContentLimit int16
	Revision     int64
	Removed      bool
}

type AnchorPresentationResult struct {
	Applied    bool
	Existed    bool
	WasRemoved bool
	IsRemoved  bool
}

func UpsertAnchorPresentationTx(tx *gorm.DB, site string, w AnchorPresentationWrite) (AnchorPresentationResult, error) {
	var rows []struct {
		Existed    bool `gorm:"column:existed"`
		WasRemoved bool `gorm:"column:was_removed"`
		IsRemoved  bool `gorm:"column:is_removed"`
	}
	title, url, workID, cover := w.Title, w.URL, w.WorkID, w.CoverHash
	if w.Removed {
		title, url, workID, cover = "", "", nil, nil
	}
	err := tx.Raw(`
		INSERT INTO community_anchor_presentation AS a (
		    site, anchor_kind, anchor_id, title, url, work_id, cover_image_hash, content_limit, revision,
		    removed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CASE WHEN ? THEN now() END, now(), now())
		ON CONFLICT (site, anchor_kind, anchor_id) DO UPDATE SET
		    title = EXCLUDED.title, url = EXCLUDED.url, work_id = EXCLUDED.work_id,
		    cover_image_hash = EXCLUDED.cover_image_hash,
		    content_limit = EXCLUDED.content_limit, revision = EXCLUDED.revision,
		    removed_at = CASE WHEN EXCLUDED.removed_at IS NULL THEN NULL
		                      ELSE COALESCE(a.removed_at, EXCLUDED.removed_at) END,
		    updated_at = now()
		  WHERE a.revision < EXCLUDED.revision
		RETURNING old.site IS NOT NULL AS existed, old.removed_at IS NOT NULL AS was_removed,
		          new.removed_at IS NOT NULL AS is_removed`,
		site, w.AnchorKind, w.AnchorID, title, url, workID, cover, w.ContentLimit, w.Revision, w.Removed,
	).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return AnchorPresentationResult{}, err
	}
	return AnchorPresentationResult{Applied: true, Existed: rows[0].Existed, WasRemoved: rows[0].WasRemoved, IsRemoved: rows[0].IsRemoved}, nil
}

type AnchorPresentationCursor struct {
	AnchorKind int16
	AnchorID   string
}

func ListAnchorPresentations(db *gorm.DB, site string, after *AnchorPresentationCursor, limit int) ([]model.CommunityAnchorPresentation, error) {
	q := db.Where("site = ?", site)
	if after != nil {
		q = q.Where("(anchor_kind, anchor_id) > (?, ?)", after.AnchorKind, after.AnchorID)
	}
	var rows []model.CommunityAnchorPresentation
	err := q.Order("anchor_kind, anchor_id").Limit(limit).Find(&rows).Error
	return rows, err
}

func PruneAnchorPresentationTombstones(db *gorm.DB, keep time.Duration) (int64, error) {
	res := db.Exec(`DELETE FROM community_anchor_presentation WHERE removed_at < ?`, time.Now().Add(-keep))
	return res.RowsAffected, res.Error
}

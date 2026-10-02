package repository

import (
	"time"

	"api/internal/platform/community/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func InsertFlagTx(tx *gorm.DB, flag *model.CommunityFlag) (bool, error) {
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(flag)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func SumPendingFlagWeightTx(tx *gorm.DB, postID int64) (float32, error) {
	var sum float32
	err := tx.Model(&model.CommunityFlag{}).
		Where("post_id = ? AND status = ?", postID, model.FlagStatusPending).
		Select("COALESCE(SUM(weight), 0)").Scan(&sum).Error
	return sum, err
}

func PendingFlaggersTx(tx *gorm.DB, postID int64) ([]int64, error) {
	var ids []int64
	err := tx.Model(&model.CommunityFlag{}).
		Where("post_id = ? AND status = ?", postID, model.FlagStatusPending).
		Pluck("flagger_id", &ids).Error
	return ids, err
}

func ResolvePendingFlagsTx(tx *gorm.DB, postID int64, status int16) error {
	return tx.Model(&model.CommunityFlag{}).
		Where("post_id = ? AND status = ?", postID, model.FlagStatusPending).
		Update("status", status).Error
}

func EnqueueReviewIfAbsentTx(tx *gorm.DB, site string, postID int64, source int16, matchedTerms []string) (int64, bool, error) {
	var existing int64
	if err := tx.Model(&model.CommunityReviewItem{}).
		Where("post_id = ? AND status = ?", postID, model.ReviewStatusPending).
		Count(&existing).Error; err != nil {
		return 0, false, err
	}
	if existing > 0 {
		return 0, false, nil
	}
	item := model.CommunityReviewItem{
		Site: &site, PostID: &postID, Source: &source, Status: model.ReviewStatusPending,
		MatchedTerms: matchedTerms,
	}
	if err := tx.Create(&item).Error; err != nil {
		return 0, false, err
	}
	return item.ID, true, nil
}

type ForwardTarget struct {
	ItemID       int64
	Site         string
	PostID       int64
	Source       *int16
	MatchedTerms datatypes.JSONSlice[string]
	AuthorID     int64
	ContentRaw   string
	Forwarded    bool
}

func LoadForwardTargetTx(tx *gorm.DB, itemID int64) (*ForwardTarget, bool, error) {
	var ft ForwardTarget
	err := tx.Table("community_review_item AS ri").
		Select("ri.id AS item_id, ri.site AS site, ri.post_id AS post_id, ri.source AS source, ri.matched_terms AS matched_terms, "+
			"(ri.trust_review_item_id IS NOT NULL) AS forwarded, p.author_id AS author_id, p.content_raw AS content_raw").
		Joins("JOIN community_post AS p ON p.id = ri.post_id").
		Where("ri.id = ? AND ri.site IS NOT NULL AND ri.post_id IS NOT NULL", itemID).
		Take(&ft).Error
	if err == gorm.ErrRecordNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &ft, true, nil
}

type ScanTarget struct {
	PostID     int64
	Site       string
	AuthorID   int64
	ContentRaw string
	PostNumber int32
	Title      string
}

func LoadScanTargetTx(tx *gorm.DB, postID int64) (*ScanTarget, bool, error) {
	var row struct {
		PostID     int64
		Site       string
		AuthorID   int64
		ContentRaw string
		PostNumber int32
		Title      *string
	}
	err := tx.Table("community_post AS p").
		Select("p.id AS post_id, t.site AS site, p.author_id AS author_id, "+
			"p.content_raw AS content_raw, p.post_number AS post_number, t.title AS title").
		Joins("JOIN community_thread AS t ON t.id = p.thread_id").
		Where("p.id = ?", postID).
		Take(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	title := ""
	if row.Title != nil {
		title = *row.Title
	}
	return &ScanTarget{
		PostID: row.PostID, Site: row.Site, AuthorID: row.AuthorID,
		ContentRaw: row.ContentRaw, PostNumber: row.PostNumber, Title: title,
	}, true, nil
}

func LockUnforwardedTx(tx *gorm.DB, limit int) ([]model.CommunityReviewItem, error) {
	var rows []model.CommunityReviewItem
	err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("trust_review_item_id IS NULL AND post_id IS NOT NULL AND site IS NOT NULL").
		Where("status = ?", model.ReviewStatusPending).
		Where("forward_after IS NULL OR forward_after <= now()").
		Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func PostBodyTx(tx *gorm.DB, postID int64) (authorID int64, contentRaw string, found bool, err error) {
	var row struct {
		AuthorID   int64
		ContentRaw string
	}
	e := tx.Table("community_post").Select("author_id, content_raw").
		Where("id = ?", postID).Take(&row).Error
	if e == gorm.ErrRecordNotFound {
		return 0, "", false, nil
	}
	if e != nil {
		return 0, "", false, e
	}
	return row.AuthorID, row.ContentRaw, true, nil
}

func SetTrustReviewItemIDTx(tx *gorm.DB, itemID, trustItemID int64) error {
	return tx.Model(&model.CommunityReviewItem{}).
		Where("id = ? AND trust_review_item_id IS NULL", itemID).
		Update("trust_review_item_id", trustItemID).Error
}

func ParkForwardTx(tx *gorm.DB, itemID int64, attempts int32) error {
	mins := 60
	if attempts < 6 {
		mins = 1 << attempts
	}
	return tx.Model(&model.CommunityReviewItem{}).Where("id = ?", itemID).Updates(map[string]any{
		"forward_attempts": attempts + 1,
		"forward_after":    time.Now().Add(time.Duration(mins) * time.Minute),
	}).Error
}

func CloseReviewItemsForPostTx(tx *gorm.DB, postID int64, status int16) error {
	return tx.Model(&model.CommunityReviewItem{}).
		Where("post_id = ? AND status = ?", postID, model.ReviewStatusPending).
		Updates(map[string]any{"status": status, "decided_at": gorm.Expr("now()")}).Error
}

type ReviewRepository struct{ db *gorm.DB }

func NewReviewRepository(db *gorm.DB) *ReviewRepository { return &ReviewRepository{db: db} }

type ReviewItemRow struct {
	ID           int64                       `gorm:"column:id"`
	Site         *string                     `gorm:"column:site"`
	PostID       *int64                      `gorm:"column:post_id"`
	Source       *int16                      `gorm:"column:source"`
	Status       int16                       `gorm:"column:status"`
	DecidedBy    *int64                      `gorm:"column:decided_by"`
	MatchedTerms datatypes.JSONSlice[string] `gorm:"column:matched_terms"`
	ThreadID     *int64                      `gorm:"column:thread_id"`
	AuthorID     *int64                      `gorm:"column:author_id"`
}

func (r *ReviewRepository) ListPending(site string, source int16, limit int) ([]ReviewItemRow, error) {
	q := r.db.Table("community_review_item AS ri").
		Select("ri.id, ri.site, ri.post_id, ri.source, ri.status, ri.decided_by, ri.matched_terms, p.thread_id, p.author_id").
		Joins("LEFT JOIN community_post AS p ON p.id = ri.post_id").
		Where("ri.site = ? AND ri.status = ?", site, model.ReviewStatusPending)
	if source >= 0 {
		q = q.Where("ri.source = ?", source)
	}
	var items []ReviewItemRow
	err := q.Order("ri.id DESC").Limit(limit).Scan(&items).Error
	return items, err
}

func GetReviewItemTx(tx *gorm.DB, id int64) (*model.CommunityReviewItem, error) {
	var item model.CommunityReviewItem
	err := tx.First(&item, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func DecideReviewItemTx(tx *gorm.DB, id int64, status int16, decidedBy int64) error {
	return tx.Model(&model.CommunityReviewItem{}).Where("id = ?", id).Updates(map[string]any{
		"status":     status,
		"decided_by": decidedBy,
		"decided_at": gorm.Expr("now()"),
	}).Error
}

type PostContext struct {
	AuthorID   int64
	Status     int16
	Site       string
	ThreadID   int64
	AnchorKind int16
	AnchorID   string
}

func PostContextTx(tx *gorm.DB, postID int64) (PostContext, bool, error) {
	var pc PostContext
	err := tx.Table("community_post AS p").
		Select("p.author_id AS author_id, p.status AS status, t.site AS site, t.id AS thread_id, t.anchor_kind AS anchor_kind, t.anchor_id AS anchor_id").
		Joins("JOIN community_thread AS t ON t.id = p.thread_id").
		Where("p.id = ?", postID).
		Take(&pc).Error
	if err == gorm.ErrRecordNotFound {
		return PostContext{}, false, nil
	}
	if err != nil {
		return PostContext{}, false, err
	}
	return pc, true, nil
}

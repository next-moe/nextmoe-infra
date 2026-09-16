package repository

import (
	"fmt"
	"time"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Filing a topic under a board takes SHARE and deleting or re-parenting the
// board takes UPDATE; that pairing is what keeps a topic off a deleted board.
const (
	LockShare  = "SHARE"
	LockUpdate = "UPDATE"
)

// Literal values, not placeholders: a partial index is only matched by a plan
// that sees the constants, and a cached generic plan never does.
var boardTopicSQL = fmt.Sprintf("community_thread.kind = %d AND community_thread.anchor_kind = %d",
	model.ThreadKindTopic, model.AnchorKindBoard)

type BoardRepository struct{ db *gorm.DB }

func NewBoardRepository(db *gorm.DB) *BoardRepository { return &BoardRepository{db: db} }

func (r *BoardRepository) ListBySite(site string) ([]model.CommunityBoard, error) {
	var rows []model.CommunityBoard
	err := r.db.Where("site = ?", site).Order("position ASC, id ASC").Find(&rows).Error
	return rows, err
}

func (r *BoardRepository) Get(site string, id int64) (*model.CommunityBoard, error) {
	return takeBoard(r.db.Where("site = ? AND id = ?", site, id))
}

func (r *BoardRepository) GetBySlug(site, slug string) (*model.CommunityBoard, error) {
	return takeBoard(r.db.Where("site = ? AND slug = ?", site, slug))
}

func (r *BoardRepository) SubtreeIDs(site string, id int64) ([]int64, error) {
	var ids []int64
	err := r.db.Model(&model.CommunityBoard{}).
		Where("site = ? AND (id = ? OR parent_id = ?)", site, id, id).
		Order("id").Pluck("id", &ids).Error
	return ids, err
}

func LockBoardTx(tx *gorm.DB, site string, id int64, strength string) (*model.CommunityBoard, error) {
	return takeBoard(tx.Clauses(clause.Locking{Strength: strength}).Where("site = ? AND id = ?", site, id))
}

func takeBoard(q *gorm.DB) (*model.CommunityBoard, error) {
	var b model.CommunityBoard
	err := q.Take(&b).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func CreateBoardTx(tx *gorm.DB, b *model.CommunityBoard) error {
	return tx.Create(b).Error
}

func UpdateBoardTx(tx *gorm.DB, id int64, updates map[string]any) error {
	updates["updated_at"] = time.Now()
	return tx.Model(&model.CommunityBoard{}).Where("id = ?", id).Updates(updates).Error
}

func DeleteBoardTx(tx *gorm.DB, id int64) error {
	return tx.Where("id = ?", id).Delete(&model.CommunityBoard{}).Error
}

func NextBoardPositionTx(tx *gorm.DB, site string, parentID *int64) (int32, error) {
	var next int32
	err := tx.Model(&model.CommunityBoard{}).
		Select("COALESCE(MAX(position) + 1, 0)").
		Where("site = ? AND parent_id IS NOT DISTINCT FROM ?", site, parentID).
		Scan(&next).Error
	return next, err
}

func LockSiblingBoardsTx(tx *gorm.DB, site string, parentID *int64) ([]model.CommunityBoard, error) {
	var rows []model.CommunityBoard
	err := tx.Clauses(clause.Locking{Strength: LockUpdate}).
		Where("site = ? AND parent_id IS NOT DISTINCT FROM ?", site, parentID).
		Order("position ASC, id ASC").Find(&rows).Error
	return rows, err
}

func SetBoardPositionTx(tx *gorm.DB, id int64, position int32) error {
	return tx.Model(&model.CommunityBoard{}).Where("id = ?", id).
		Updates(map[string]any{"position": position, "updated_at": time.Now()}).Error
}

func BoardHasChildrenTx(tx *gorm.DB, id int64) (bool, error) {
	var found bool
	err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM community_board WHERE parent_id = ?)`, id).Scan(&found).Error
	return found, err
}

func BoardHasThreadsTx(tx *gorm.DB, board *model.CommunityBoard) (bool, error) {
	var found bool
	err := tx.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM community_thread
			 WHERE anchor_kind = ? AND anchor_id = ? AND site = ?)`,
		model.AnchorKindBoard, model.BoardAnchorID(board.ID), board.Site).Scan(&found).Error
	return found, err
}

func TopicBoardTx(tx *gorm.DB, thread *model.CommunityThread) (*model.CommunityBoard, error) {
	id, ok := model.BoardIDFromAnchor(thread.AnchorKind, thread.AnchorID)
	if !ok {
		return nil, nil
	}
	return takeBoard(tx.Where("site = ? AND id = ?", thread.Site, id))
}

type BoardStatsRow struct {
	TopicsCount     int64
	PostsCount      int64
	LastPostedAt    *time.Time
	LastThreadID    *int64
	LastThreadTitle *string
}

func listedTopics(db *gorm.DB, site string) *gorm.DB {
	return db.Table("community_thread").
		Joins("JOIN community_post ON community_post.thread_id = community_thread.id AND community_post.post_number = 1").
		Where(boardTopicSQL).
		Where("community_thread.site = ?", site).
		Where("community_thread.status IN ?", []int16{model.ThreadStatusOpen, model.ThreadStatusClosed}).
		Where("community_post.status = ?", model.PostStatusVisible)
}

func (r *BoardRepository) SiteStats(site string) (map[int64]BoardStatsRow, error) {
	return r.stats(func() *gorm.DB { return listedTopics(r.db, site) })
}

func (r *BoardRepository) BoardStats(site string, boardID int64) (BoardStatsRow, error) {
	rows, err := r.stats(func() *gorm.DB {
		return listedTopics(r.db, site).Where("community_thread.anchor_id = ?", model.BoardAnchorID(boardID))
	})
	return rows[boardID], err
}

func (r *BoardRepository) stats(scope func() *gorm.DB) (map[int64]BoardStatsRow, error) {
	var aggs []struct {
		AnchorID     string     `gorm:"column:anchor_id"`
		TopicsCount  int64      `gorm:"column:topics_count"`
		PostsCount   int64      `gorm:"column:posts_count"`
		LastPostedAt *time.Time `gorm:"column:last_posted_at"`
	}
	if err := scope().
		Select("community_thread.anchor_id, COUNT(*) AS topics_count, " +
			"COALESCE(SUM(community_thread.posts_count), 0) AS posts_count, " +
			"MAX(community_thread.last_posted_at) AS last_posted_at").
		Group("community_thread.anchor_id").Scan(&aggs).Error; err != nil {
		return nil, err
	}
	var latest []struct {
		AnchorID string  `gorm:"column:anchor_id"`
		ID       int64   `gorm:"column:id"`
		Title    *string `gorm:"column:title"`
	}
	if err := scope().
		Select("DISTINCT ON (community_thread.anchor_id) community_thread.anchor_id, community_thread.id, community_thread.title").
		Order("community_thread.anchor_id, community_thread.last_posted_at DESC NULLS LAST, community_thread.id DESC").
		Scan(&latest).Error; err != nil {
		return nil, err
	}

	out := make(map[int64]BoardStatsRow, len(aggs))
	for _, a := range aggs {
		id, ok := model.BoardIDFromAnchor(model.AnchorKindBoard, a.AnchorID)
		if !ok {
			continue
		}
		out[id] = BoardStatsRow{TopicsCount: a.TopicsCount, PostsCount: a.PostsCount, LastPostedAt: a.LastPostedAt}
	}
	for _, l := range latest {
		id, ok := model.BoardIDFromAnchor(model.AnchorKindBoard, l.AnchorID)
		if !ok {
			continue
		}
		row := out[id]
		threadID := l.ID
		row.LastThreadID, row.LastThreadTitle = &threadID, l.Title
		out[id] = row
	}
	return out, nil
}

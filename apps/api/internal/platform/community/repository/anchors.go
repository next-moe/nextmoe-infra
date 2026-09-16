package repository

import (
	"strings"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

type AnchorKey struct {
	Kind int16
	ID   string
}

func UpsertAnchorUserTx(tx *gorm.DB, site string, userID int64, anchorKind int16, anchorID string, level int16) error {
	return tx.Exec(`
		INSERT INTO community_anchor_user (site, user_id, anchor_kind, anchor_id, notification_level, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, now(), now())
		ON CONFLICT (site, user_id, anchor_kind, anchor_id) DO UPDATE SET
		       notification_level = EXCLUDED.notification_level,
		       updated_at = now()`,
		site, userID, anchorKind, anchorID, level).Error
}

func DeleteAnchorUserTx(tx *gorm.DB, site string, userID int64, anchorKind int16, anchorID string) error {
	return tx.Where("site = ? AND user_id = ? AND anchor_kind = ? AND anchor_id = ?",
		site, userID, anchorKind, anchorID).Delete(&model.CommunityAnchorUser{}).Error
}

func DeleteBoardAnchorUsersTx(tx *gorm.DB, site string, boardID int64) (int64, error) {
	res := tx.Exec(`
		DELETE FROM community_anchor_user
		 WHERE site = ? AND anchor_kind = ? AND anchor_id = ?`,
		site, model.AnchorKindBoard, model.BoardAnchorID(boardID))
	return res.RowsAffected, res.Error
}

func (r *EngagementRepository) AnchorStates(site string, userID int64, anchors []AnchorKey) ([]model.CommunityAnchorUser, error) {
	if len(anchors) == 0 {
		return nil, nil
	}
	var b strings.Builder
	args := make([]any, 0, 2+len(anchors)*2)
	args = append(args, site, userID)
	b.WriteString("site = ? AND user_id = ? AND (anchor_kind, anchor_id) IN (")
	for i, a := range anchors {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("(?,?)")
		args = append(args, a.Kind, a.ID)
	}
	b.WriteByte(')')
	var rows []model.CommunityAnchorUser
	err := r.db.Model(&model.CommunityAnchorUser{}).Where(b.String(), args...).Find(&rows).Error
	return rows, err
}

func (r *EngagementRepository) ListAnchorSubscriptions(site string, userID int64, anchorKind int16, afterID int64, limit int) ([]model.CommunityAnchorUser, error) {
	q := r.db.Model(&model.CommunityAnchorUser{}).Where("site = ? AND user_id = ?", site, userID)
	if anchorKind >= 0 {
		q = q.Where("anchor_kind = ?", anchorKind)
	}
	if afterID > 0 {
		q = q.Where("id < ?", afterID)
	}
	var rows []model.CommunityAnchorUser
	err := q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

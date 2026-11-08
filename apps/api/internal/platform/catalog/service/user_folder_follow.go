package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrFolderFollowOwn   = stderrors.New("catalog: a folder cannot be followed by its owner")
	ErrFolderFollowLimit = stderrors.New("catalog: followed-folder cap reached")
)

const (
	readableFolder         = "visibility IN (1, 2)"
	readableFollowedFolder = "EXISTS (SELECT 1 FROM catalog_user_folder f WHERE f.id = catalog_user_folder_follow.folder_id AND f.visibility IN (1, 2))"
)

func (s *UserFolderService) Follow(ctx context.Context, uid, folderID int64, clientID, site string) (*model.CatalogUserFolderFollow, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	var out model.CatalogUserFolderFollow
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var folder model.CatalogUserFolder
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", folderID).Where(readableFolder).Take(&folder).Error
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return ErrFolderNotFound
		}
		if err != nil {
			return err
		}
		if folder.OwnerUID == uid {
			return ErrFolderFollowOwn
		}
		err = tx.Where("actor_uid = ? AND folder_id = ?", uid, folderID).Take(&out).Error
		if err == nil {
			return nil
		}
		if !stderrors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var n int64
		if err := tx.Model(&model.CatalogUserFolderFollow{}).Where("actor_uid = ?", uid).Count(&n).Error; err != nil {
			return err
		}
		if n >= model.FolderFollowsPerUserMax {
			return ErrFolderFollowLimit
		}
		out = model.CatalogUserFolderFollow{ActorUID: uid, FolderID: folderID, ClientID: clientID, Site: site, SeenAt: time.Now()}
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		return tx.Model(&model.CatalogUserFolder{}).Where("id = ?", folderID).
			UpdateColumn("follower_count", gorm.Expr("follower_count + 1")).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *UserFolderService) Unfollow(ctx context.Context, uid, folderID int64) error {
	if uid <= 0 {
		return ErrFolderActorRequired
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("actor_uid = ? AND folder_id = ?", uid, folderID).Delete(&model.CatalogUserFolderFollow{})
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		return tx.Model(&model.CatalogUserFolder{}).Where("id = ?", folderID).
			UpdateColumn("follower_count", gorm.Expr("GREATEST(follower_count - 1, 0)")).Error
	})
}

func (s *UserFolderService) ListFollows(ctx context.Context, uid, beforeID int64, limit int) ([]model.CatalogUserFolderFollow, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	q := s.db.WithContext(ctx).Where("actor_uid = ?", uid).Where(readableFollowedFolder)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []model.CatalogUserFolderFollow
	err := q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *UserFolderService) CountFollows(ctx context.Context, uid int64) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogUserFolderFollow{}).
		Where("actor_uid = ?", uid).Where(readableFollowedFolder).Count(&n).Error
	return n, err
}

func (s *UserFolderService) ListFollowsFor(ctx context.Context, uid int64, folderIDs []int64) ([]model.CatalogUserFolderFollow, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	if len(folderIDs) == 0 {
		return nil, nil
	}
	var rows []model.CatalogUserFolderFollow
	err := s.db.WithContext(ctx).Where("actor_uid = ? AND folder_id IN ?", uid, folderIDs).
		Where(readableFollowedFolder).Find(&rows).Error
	return rows, err
}

func purgeFollowerTx(tx *gorm.DB, uid int64) (int64, error) {
	var folderIDs []int64
	if err := tx.Model(&model.CatalogUserFolderFollow{}).Where("actor_uid = ?", uid).
		Pluck("folder_id", &folderIDs).Error; err != nil {
		return 0, err
	}
	if len(folderIDs) == 0 {
		return 0, nil
	}
	res := tx.Where("actor_uid = ?", uid).Delete(&model.CatalogUserFolderFollow{})
	if res.Error != nil {
		return 0, res.Error
	}
	if err := tx.Exec(`
		UPDATE catalog_user_folder f
		   SET follower_count = (SELECT count(*) FROM catalog_user_folder_follow x WHERE x.folder_id = f.id)
		 WHERE f.id IN ?`, folderIDs).Error; err != nil {
		return 0, err
	}
	return res.RowsAffected, nil
}

func (s *UserFolderService) ReadableFoldersByID(ctx context.Context, ids []int64) ([]model.CatalogUserFolder, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []model.CatalogUserFolder
	err := s.db.WithContext(ctx).Where("id IN ?", ids).Where(readableFolder).Find(&rows).Error
	return rows, err
}

// MarkFollowSeen moves the follower's watermark forward only, and never past
// now, so a stale tab cannot bring back updates the follower already saw.
func (s *UserFolderService) MarkFollowSeen(ctx context.Context, uid, folderID int64, at *time.Time) (*model.CatalogUserFolderFollow, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	now := time.Now()
	mark := now
	if at != nil && at.Before(now) {
		mark = *at
	}
	var out model.CatalogUserFolderFollow
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.CatalogUserFolderFollow{}).
			Where("actor_uid = ? AND folder_id = ?", uid, folderID).Where(readableFollowedFolder).
			UpdateColumn("seen_at", gorm.Expr("GREATEST(seen_at, ?)", mark)).Error; err != nil {
			return err
		}
		err := tx.Where("actor_uid = ? AND folder_id = ?", uid, folderID).Where(readableFollowedFolder).Take(&out).Error
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return ErrFolderNotFound
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

const followNewItemsSQL = `(SELECT count(*) FROM (SELECT 1 FROM catalog_user_folder_item i
	JOIN catalog_work w ON w.id = i.work_id AND w.deleted_at IS NULL
	WHERE i.folder_id = catalog_user_folder_follow.folder_id AND i.created_at > catalog_user_folder_follow.seen_at
	LIMIT %d) n)`

// FollowNewItems counts, per followed folder, the works added since the
// follower last opened it, stopping at model.FolderNewItemsCap.
func (s *UserFolderService) FollowNewItems(ctx context.Context, uid int64, folderIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(folderIDs))
	if uid <= 0 || len(folderIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		FolderID int64
		N        int
	}
	err := s.db.WithContext(ctx).Model(&model.CatalogUserFolderFollow{}).
		Select("folder_id, "+fmt.Sprintf(followNewItemsSQL, model.FolderNewItemsCap)+" AS n").
		Where("actor_uid = ? AND folder_id IN ?", uid, folderIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.FolderID] = r.N
	}
	return out, nil
}

// CountUpdatedFollows is the red dot: followed, readable folders that gained
// a work since the follower last opened them, stopping at the cap.
func (s *UserFolderService) CountUpdatedFollows(ctx context.Context, uid int64) (int, error) {
	if uid <= 0 {
		return 0, ErrFolderActorRequired
	}
	var n int
	err := s.db.WithContext(ctx).Raw(`
		SELECT count(*) FROM (
		  SELECT 1 FROM catalog_user_folder_follow
		   WHERE actor_uid = ? AND `+readableFollowedFolder+`
		     AND EXISTS (SELECT 1 FROM catalog_user_folder_item i
		                   JOIN catalog_work w ON w.id = i.work_id AND w.deleted_at IS NULL
		                  WHERE i.folder_id = catalog_user_folder_follow.folder_id
		                    AND i.created_at > catalog_user_folder_follow.seen_at)
		   LIMIT ?) u`, uid, model.FolderNewItemsCap).Scan(&n).Error
	return n, err
}

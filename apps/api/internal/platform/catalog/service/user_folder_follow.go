package service

import (
	"context"
	stderrors "errors"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrFolderFollowOwn   = stderrors.New("catalog: a folder cannot be followed by its owner")
	ErrFolderFollowLimit = stderrors.New("catalog: followed-folder cap reached")
)

const publicFollowedFolder = "EXISTS (SELECT 1 FROM catalog_user_folder f WHERE f.id = catalog_user_folder_follow.folder_id AND f.visibility = 1)"

func (s *UserFolderService) Follow(ctx context.Context, uid, folderID int64, clientID, site string) (*model.CatalogUserFolderFollow, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	var out model.CatalogUserFolderFollow
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var folder model.CatalogUserFolder
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND visibility = ?", folderID, model.FolderVisibilityPublic).Take(&folder).Error
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
		out = model.CatalogUserFolderFollow{ActorUID: uid, FolderID: folderID, ClientID: clientID, Site: site}
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
	q := s.db.WithContext(ctx).Where("actor_uid = ?", uid).Where(publicFollowedFolder)
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
		Where("actor_uid = ?", uid).Where(publicFollowedFolder).Count(&n).Error
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
		Where(publicFollowedFolder).Find(&rows).Error
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

func (s *UserFolderService) PublicFoldersByID(ctx context.Context, ids []int64) ([]model.CatalogUserFolder, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []model.CatalogUserFolder
	err := s.db.WithContext(ctx).Where("id IN ? AND visibility = ?", ids, model.FolderVisibilityPublic).Find(&rows).Error
	return rows, err
}

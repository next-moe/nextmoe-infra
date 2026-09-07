package service

import (
	"context"
	stderrors "errors"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// The public lane never widens for the owner: a folder is on it while its
// owner keeps visibility=public and never otherwise, so the same folder id
// answers the same bytes to everyone and /v2/me/folders stays the one face
// that knows about private folders.
func (s *UserFolderService) ListPublic(ctx context.Context, ownerUID, sinceID int64, limit int) ([]model.CatalogUserFolder, error) {
	q := s.db.WithContext(ctx).
		Where("owner_uid = ? AND visibility = ?", ownerUID, model.FolderVisibilityPublic)
	if sinceID > 0 {
		q = q.Where("id > ?", sinceID)
	}
	var rows []model.CatalogUserFolder
	err := q.Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *UserFolderService) CountPublic(ctx context.Context, ownerUID int64) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogUserFolder{}).
		Where("owner_uid = ? AND visibility = ?", ownerUID, model.FolderVisibilityPublic).
		Count(&n).Error
	return n, err
}

func (s *UserFolderService) GetPublic(ctx context.Context, folderID int64) (*model.CatalogUserFolder, error) {
	var row model.CatalogUserFolder
	err := s.db.WithContext(ctx).
		Where("id = ? AND visibility = ?", folderID, model.FolderVisibilityPublic).
		Take(&row).Error
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrFolderNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Ordered and paginated exactly like the owner's lane, so a client that walks
// a folder it can also own reads one shape.
func (s *UserFolderService) ListPublicItems(ctx context.Context, folderID int64, since time.Time, sinceWorkID int64, limit int) ([]model.CatalogUserFolderItem, error) {
	if _, err := s.GetPublic(ctx, folderID); err != nil {
		return nil, err
	}
	q := s.db.WithContext(ctx).Where("folder_id = ?", folderID)
	if !since.IsZero() {
		if sinceWorkID > 0 {
			q = q.Where("updated_at > ? OR (updated_at = ? AND work_id > ?)", since, since, sinceWorkID)
		} else {
			q = q.Where("updated_at > ?", since)
		}
	}
	var rows []model.CatalogUserFolderItem
	err := q.Order("updated_at ASC, work_id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *UserFolderService) CountPublicItems(ctx context.Context, folderID int64) (int64, error) {
	if _, err := s.GetPublic(ctx, folderID); err != nil {
		return 0, err
	}
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogUserFolderItem{}).
		Where("folder_id = ?", folderID).Count(&n).Error
	return n, err
}

// Answers "which of my folders hold this work" in one query. Without it the
// add-to-folder picker had to read every folder the user owns (up to 200) and
// probe each one.
func (s *UserFolderService) ListMineContaining(ctx context.Context, uid, workID, sinceID int64, limit int) ([]model.CatalogUserFolder, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	q := s.db.WithContext(ctx).Model(&model.CatalogUserFolder{}).
		Joins("JOIN catalog_user_folder_item i ON i.folder_id = catalog_user_folder.id AND i.work_id = ?", workID).
		Where("catalog_user_folder.owner_uid = ?", uid)
	if sinceID > 0 {
		q = q.Where("catalog_user_folder.id > ?", sinceID)
	}
	var rows []model.CatalogUserFolder
	err := q.Order("catalog_user_folder.id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *UserFolderService) CountMineContaining(ctx context.Context, uid, workID int64) (int64, error) {
	if uid <= 0 {
		return 0, ErrFolderActorRequired
	}
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogUserFolder{}).
		Joins("JOIN catalog_user_folder_item i ON i.folder_id = catalog_user_folder.id AND i.work_id = ?", workID).
		Where("catalog_user_folder.owner_uid = ?", uid).Count(&n).Error
	return n, err
}

func (s *UserFolderService) ModerationGet(ctx context.Context, folderID int64) (*model.CatalogUserFolder, error) {
	var row model.CatalogUserFolder
	err := s.db.WithContext(ctx).Take(&row, "id = ?", folderID).Error
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrFolderNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Name, description and visibility only: a moderator acts on the text a folder
// publishes, never on which works it holds and never on the default flag,
// which is the owner's own navigation.
func (s *UserFolderService) ModerationPatch(ctx context.Context, folderID int64, p FolderPatch) (*model.CatalogUserFolder, error) {
	if p.Name == nil && p.Description == nil && p.Visibility == nil {
		return nil, ErrFolderNothingToUpdate
	}
	var row model.CatalogUserFolder
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Take(&row, "id = ?", folderID).Error; err != nil {
			if stderrors.Is(err, gorm.ErrRecordNotFound) {
				return ErrFolderNotFound
			}
			return err
		}
		updates := map[string]any{"updated_at": time.Now()}
		if p.Name != nil {
			// Blanking is the point: the owner path refuses an empty name, but
			// the abuse a moderator removes is usually the name itself and the
			// owner is the one who has to pick a new one.
			updates["name"] = *p.Name
		}
		if p.Description != nil {
			updates["description"] = *p.Description
		}
		if p.Visibility != nil {
			if *p.Visibility != model.FolderVisibilityPrivate && *p.Visibility != model.FolderVisibilityPublic {
				return ErrFolderBadVisibility
			}
			updates["visibility"] = *p.Visibility
		}
		if err := tx.Model(&model.CatalogUserFolder{}).Where("id = ?", folderID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Take(&row, "id = ?", folderID).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Unlike the owner's delete this one accepts the default folder. The flag is
// set by the folder's owner, so refusing it here would let anyone make an
// abusive folder undeletable by marking it default.
func (s *UserFolderService) ModerationDelete(ctx context.Context, folderID int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.CatalogUserFolder
		if err := tx.Take(&row, "id = ?", folderID).Error; err != nil {
			if stderrors.Is(err, gorm.ErrRecordNotFound) {
				return ErrFolderNotFound
			}
			return err
		}
		if err := tx.Where("folder_id = ?", folderID).Delete(&model.CatalogUserFolderItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.CatalogUserFolder{}, folderID).Error
	})
}

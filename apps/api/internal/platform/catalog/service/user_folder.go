package service

import (
	"context"
	stderrors "errors"
	"strings"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrFolderActorRequired   = stderrors.New("catalog: a folder operation requires a user")
	ErrFolderNotFound        = stderrors.New("catalog: folder not found")
	ErrFolderNameRequired    = stderrors.New("catalog: folder name is required")
	ErrFolderBadVisibility   = stderrors.New("catalog: unknown folder visibility")
	ErrFolderLimit           = stderrors.New("catalog: too many folders")
	ErrFolderItemLimit       = stderrors.New("catalog: folder is full")
	ErrFolderWorkUnavailable = stderrors.New("catalog: work not available for folders")
	ErrFolderDefaultDeletion = stderrors.New("catalog: the default folder cannot be deleted")
	ErrFolderDefaultOnly     = stderrors.New("catalog: is_default can only be set, not cleared")
	ErrFolderNothingToUpdate = stderrors.New("catalog: nothing to update")
	ErrFolderCoverNotMember  = stderrors.New("catalog: the cover work is not in the folder")
)

type UserFolderService struct{ db *gorm.DB }

func NewUserFolderService(db *gorm.DB) *UserFolderService {
	return &UserFolderService{db: db}
}

type FolderCreate struct {
	OwnerUID    int64
	Name        string
	Description string
	Visibility  int16
	IsDefault   bool
	Tags        []string
}

type FolderPatch struct {
	Name        *string
	Description *string
	Visibility  *int16
	IsDefault   *bool
	Tags        *[]string
	// CoverWorkID 0 clears the pick and the folder falls back to its works.
	CoverWorkID *int64
}

func (s *UserFolderService) Create(ctx context.Context, in FolderCreate) (*model.CatalogUserFolder, error) {
	if in.OwnerUID <= 0 {
		return nil, ErrFolderActorRequired
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, ErrFolderNameRequired
	}
	if !model.FolderVisibilityValid(in.Visibility) {
		return nil, ErrFolderBadVisibility
	}
	tags, err := NormalizeFolderTags(in.Tags)
	if err != nil {
		return nil, err
	}
	row := model.CatalogUserFolder{
		OwnerUID: in.OwnerUID, Name: in.Name, Description: in.Description,
		Visibility: in.Visibility, IsDefault: in.IsDefault,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.CatalogUserFolder{}).
			Where("owner_uid = ?", in.OwnerUID).Count(&n).Error; err != nil {
			return err
		}
		if n >= model.FoldersPerUserMax {
			return ErrFolderLimit
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := setFolderTagsTx(tx, row.ID, tags); err != nil {
			return err
		}
		if in.IsDefault {
			return demoteOtherDefaults(tx, in.OwnerUID, row.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *UserFolderService) Patch(ctx context.Context, uid, folderID int64, p FolderPatch) (*model.CatalogUserFolder, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	if p.Name == nil && p.Description == nil && p.Visibility == nil && p.IsDefault == nil && p.Tags == nil && p.CoverWorkID == nil {
		return nil, ErrFolderNothingToUpdate
	}
	var tags []string
	if p.Tags != nil {
		var err error
		if tags, err = NormalizeFolderTags(*p.Tags); err != nil {
			return nil, err
		}
	}
	var row model.CatalogUserFolder
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ownedFolder(tx, uid, folderID, &row); err != nil {
			return err
		}
		updates := map[string]any{"updated_at": time.Now()}
		if p.Name != nil {
			name := strings.TrimSpace(*p.Name)
			if name == "" {
				return ErrFolderNameRequired
			}
			updates["name"] = name
		}
		if p.Description != nil {
			updates["description"] = *p.Description
		}
		if p.Visibility != nil {
			if !model.FolderVisibilityValid(*p.Visibility) {
				return ErrFolderBadVisibility
			}
			updates["visibility"] = *p.Visibility
		}
		if p.IsDefault != nil {
			if !*p.IsDefault {
				return ErrFolderDefaultOnly
			}
			updates["is_default"] = true
		}
		if err := tx.Model(&model.CatalogUserFolder{}).Where("id = ?", folderID).Updates(updates).Error; err != nil {
			return err
		}
		if p.IsDefault != nil {
			if err := demoteOtherDefaults(tx, uid, folderID); err != nil {
				return err
			}
		}
		if p.Tags != nil {
			if err := setFolderTagsTx(tx, folderID, tags); err != nil {
				return err
			}
		}
		if p.CoverWorkID != nil {
			if err := setFolderCoverTx(tx, folderID, *p.CoverWorkID); err != nil {
				return err
			}
		}
		return tx.Take(&row, "id = ?", folderID).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *UserFolderService) Delete(ctx context.Context, uid, folderID int64) error {
	if uid <= 0 {
		return ErrFolderActorRequired
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row model.CatalogUserFolder
		if err := ownedFolder(tx, uid, folderID, &row); err != nil {
			return err
		}
		if row.IsDefault {
			return ErrFolderDefaultDeletion
		}
		return deleteFolderRows(tx, folderID)
	})
}

func deleteFolderRows(tx *gorm.DB, folderID int64) error {
	if err := tx.Where("folder_id = ?", folderID).Delete(&model.CatalogUserFolderItem{}).Error; err != nil {
		return err
	}
	if err := tx.Where("folder_id = ?", folderID).Delete(&model.CatalogUserFolderFollow{}).Error; err != nil {
		return err
	}
	if err := tx.Where("folder_id = ?", folderID).Delete(&model.CatalogUserFolderTag{}).Error; err != nil {
		return err
	}
	if err := tx.Where("folder_id = ?", folderID).Delete(&model.CatalogUserFolderImport{}).Error; err != nil {
		return err
	}
	return tx.Delete(&model.CatalogUserFolder{}, folderID).Error
}

func (s *UserFolderService) Get(ctx context.Context, uid, folderID int64) (*model.CatalogUserFolder, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	var row model.CatalogUserFolder
	if err := ownedFolder(s.db.WithContext(ctx), uid, folderID, &row); err != nil {
		if stderrors.Is(err, ErrFolderNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (s *UserFolderService) ListMine(ctx context.Context, uid, sinceID int64, limit int) ([]model.CatalogUserFolder, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	q := s.db.WithContext(ctx).Where("owner_uid = ?", uid)
	if sinceID > 0 {
		q = q.Where("id > ?", sinceID)
	}
	var rows []model.CatalogUserFolder
	err := q.Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *UserFolderService) CountMine(ctx context.Context, uid int64) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogUserFolder{}).
		Where("owner_uid = ?", uid).Count(&n).Error
	return n, err
}

func (s *UserFolderService) ListItems(ctx context.Context, uid, folderID int64, p FolderItemPage) ([]model.CatalogUserFolderItem, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	if err := ownedFolder(s.db.WithContext(ctx), uid, folderID, &model.CatalogUserFolder{}); err != nil {
		return nil, err
	}
	return folderItemsPage(s.db.WithContext(ctx), folderID, p)
}

func (s *UserFolderService) CountItems(ctx context.Context, uid, folderID int64) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogUserFolderItem{}).
		Where("folder_id = ?", folderID).Where(liveWorkCond("catalog_user_folder_item.work_id")).Count(&n).Error
	return n, err
}

func (s *UserFolderService) PutItem(ctx context.Context, uid, folderID, workID int64) (*model.CatalogUserFolderItem, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	var out model.CatalogUserFolderItem
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var folder model.CatalogUserFolder
		if err := ownedFolder(tx.Clauses(clause.Locking{Strength: "UPDATE"}), uid, folderID, &folder); err != nil {
			return err
		}
		if err := assertFolderableWork(tx, workID); err != nil {
			return err
		}
		if folder.ItemCount >= model.FolderItemsMax {
			return ErrFolderItemLimit
		}
		pos, err := nextFolderItemPosition(tx, folderID)
		if err != nil {
			return err
		}
		row := model.CatalogUserFolderItem{FolderID: folderID, WorkID: workID, OwnerUID: uid, Position: pos}
		res := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "folder_id"}, {Name: "work_id"}},
			DoNothing: true,
		}).Create(&row)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			out = row
			return tx.Model(&model.CatalogUserFolder{}).Where("id = ?", folderID).
				Updates(map[string]any{"item_count": gorm.Expr("item_count + 1"), "updated_at": time.Now()}).Error
		}
		// Re-adding an existing membership is a pure no-op: touching updated_at
		// here would re-deliver the row on every other client's incremental pull.
		return tx.Take(&out, "folder_id = ? AND work_id = ?", folderID, workID).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *UserFolderService) DeleteItem(ctx context.Context, uid, folderID, workID int64) error {
	if uid <= 0 {
		return ErrFolderActorRequired
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ownedFolder(tx, uid, folderID, &model.CatalogUserFolder{}); err != nil {
			return err
		}
		res := tx.Where("folder_id = ? AND work_id = ?", folderID, workID).
			Delete(&model.CatalogUserFolderItem{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			return tx.Model(&model.CatalogUserFolder{}).Where("id = ?", folderID).
				Updates(map[string]any{"item_count": gorm.Expr("GREATEST(item_count - 1, 0)"), "updated_at": time.Now()}).Error
		}
		return nil
	})
}

func demoteOtherDefaults(tx *gorm.DB, uid, keepID int64) error {
	return tx.Model(&model.CatalogUserFolder{}).
		Where("owner_uid = ? AND is_default AND id <> ?", uid, keepID).
		Updates(map[string]any{"is_default": false, "updated_at": time.Now()}).Error
}

func ownedFolder(tx *gorm.DB, uid, folderID int64, dst *model.CatalogUserFolder) error {
	err := tx.Where("id = ? AND owner_uid = ?", folderID, uid).Take(dst).Error
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return ErrFolderNotFound
	}
	return err
}

func assertFolderableWork(tx *gorm.DB, workID int64) error {
	var work model.CatalogWork
	err := tx.Select("status").Where("id = ?", workID).Take(&work).Error
	switch {
	case stderrors.Is(err, gorm.ErrRecordNotFound):
		return ErrFolderWorkUnavailable
	case err != nil:
		return err
	case work.Status != model.WorkStatusLive:
		return ErrFolderWorkUnavailable
	}
	return nil
}

func setFolderCoverTx(tx *gorm.DB, folderID, workID int64) error {
	if workID > 0 {
		var item model.CatalogUserFolderItem
		if err := folderItem(tx, folderID, workID, &item); err != nil {
			if stderrors.Is(err, ErrFolderItemNotFound) {
				return ErrFolderCoverNotMember
			}
			return err
		}
		if item.IsCover {
			return nil
		}
	}
	if err := tx.Model(&model.CatalogUserFolderItem{}).Where("folder_id = ? AND is_cover", folderID).
		UpdateColumn("is_cover", false).Error; err != nil {
		return err
	}
	if workID == 0 {
		return nil
	}
	return tx.Model(&model.CatalogUserFolderItem{}).Where("folder_id = ? AND work_id = ?", folderID, workID).
		UpdateColumn("is_cover", true).Error
}

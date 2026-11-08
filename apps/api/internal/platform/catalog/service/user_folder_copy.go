package service

import (
	"context"
	stderrors "errors"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// Copy makes a private folder of the bearer's own from a folder they can read.
// The source is named at the top of the description, so the copier decides
// whether to keep the credit.
func (s *UserFolderService) Copy(ctx context.Context, uid, sourceID int64, name string) (*model.CatalogUserFolder, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	var row model.CatalogUserFolder
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var src model.CatalogUserFolder
		err := tx.Where("id = ? AND ("+readableFolder+" OR owner_uid = ?)", sourceID, uid).Take(&src).Error
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return ErrFolderNotFound
		}
		if err != nil {
			return err
		}
		name = strings.TrimSpace(name)
		if name == "" {
			name = strings.TrimSpace(src.Name)
		}
		if name == "" {
			return ErrFolderNameRequired
		}
		var n int64
		if err := tx.Model(&model.CatalogUserFolder{}).Where("owner_uid = ?", uid).Count(&n).Error; err != nil {
			return err
		}
		if n >= model.FoldersPerUserMax {
			return ErrFolderLimit
		}
		row = model.CatalogUserFolder{
			OwnerUID: uid, Name: name, Description: copiedDescription(src),
			Visibility: model.FolderVisibilityPrivate, CopiedFromID: &src.ID,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		res := tx.Exec(`
			INSERT INTO catalog_user_folder_item (folder_id, work_id, owner_uid, position, note, is_cover, created_at, updated_at)
			SELECT ?, i.work_id, ?, i.position, i.note, i.is_cover, now(), now()
			  FROM catalog_user_folder_item i
			 WHERE i.folder_id = ? AND `+liveWorkCond("i.work_id"), row.ID, uid, src.ID)
		if res.Error != nil {
			return res.Error
		}
		if err := tx.Exec(`
			INSERT INTO catalog_user_folder_tag (folder_id, tag, position)
			SELECT ?, tag, position FROM catalog_user_folder_tag WHERE folder_id = ?`, row.ID, src.ID).Error; err != nil {
			return err
		}
		row.ItemCount = int(res.RowsAffected)
		return tx.Model(&model.CatalogUserFolder{}).Where("id = ?", row.ID).
			UpdateColumn("item_count", row.ItemCount).Error
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func copiedDescription(src model.CatalogUserFolder) string {
	credit := "源自「" + strings.TrimSpace(src.Name) + "」"
	if strings.TrimSpace(src.Name) == "" {
		credit = "源自收藏夹 #" + strconv.FormatInt(src.ID, 10)
	}
	if src.Description != "" {
		credit += "\n\n" + src.Description
	}
	if r := []rune(credit); len(r) > 500 {
		credit = string(r[:500])
	}
	return credit
}

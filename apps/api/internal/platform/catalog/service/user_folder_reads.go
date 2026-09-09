package service

import (
	"context"

	"api/internal/platform/catalog/model"
)

type FolderHolding struct {
	WorkID   int64
	FolderID int64
}

func (s *UserFolderService) Holdings(ctx context.Context, uid int64, workIDs []int64) ([]FolderHolding, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	if len(workIDs) == 0 {
		return nil, nil
	}
	var rows []FolderHolding
	err := s.db.WithContext(ctx).
		Model(&model.CatalogUserFolderItem{}).
		Select("catalog_user_folder_item.work_id AS work_id, catalog_user_folder_item.folder_id AS folder_id").
		Joins("JOIN catalog_user_folder f ON f.id = catalog_user_folder_item.folder_id").
		Where("f.owner_uid = ? AND catalog_user_folder_item.work_id IN ?", uid, workIDs).
		Order("catalog_user_folder_item.work_id ASC, catalog_user_folder_item.folder_id ASC").
		Scan(&rows).Error
	return rows, err
}

// Ownership is read from the folder, not from catalog_user_folder_item.owner_uid:
// the item column is a denormalised copy filled by the write path, and the
// callers of this lane fan notifications out to the people it names. The folder
// row is the only authority on who owns a membership.
func (s *UserFolderService) HoldersOfWork(ctx context.Context, workID, sinceUID int64, limit int) ([]int64, error) {
	q := s.db.WithContext(ctx).
		Model(&model.CatalogUserFolder{}).
		Distinct().
		Joins("JOIN catalog_user_folder_item i ON i.folder_id = catalog_user_folder.id AND i.work_id = ?", workID)
	if sinceUID > 0 {
		q = q.Where("catalog_user_folder.owner_uid > ?", sinceUID)
	}
	var out []int64
	err := q.Order("catalog_user_folder.owner_uid ASC").Limit(limit).
		Pluck("catalog_user_folder.owner_uid", &out).Error
	return out, err
}

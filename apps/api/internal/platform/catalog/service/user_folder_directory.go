package service

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type FolderSort int8

const (
	FolderSortID FolderSort = iota
	FolderSortFollowers
	FolderSortUpdated
	FolderSortCreated
)

type FolderCursor struct {
	Set       bool
	ID        int64
	Followers int
	Updated   time.Time
}

type PublicFolderQuery struct {
	OwnerUID       int64
	ContainsWorkID int64
	NSFW           bool
	Sort           FolderSort
	Cursor         FolderCursor
	Limit          int
}

// OFFSET 0 keeps the planner from pulling the EXISTS up into an anti join.
// Pulled up, the directory's NOT EXISTS hash-joined every folder item against
// catalog_work (122ms locally on 394k items); as a per-folder subplan it stops
// at the first r18 work, which ~97% of works are (3ms).
func folderHoldsRated(rating int16) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM catalog_user_folder_item ri JOIN catalog_work rw ON rw.id = ri.work_id
		WHERE ri.folder_id = catalog_user_folder.id AND rw.deleted_at IS NULL AND rw.content_rating = %d OFFSET 0)`, rating)
}

func (s *UserFolderService) FolderRecentWorks(ctx context.Context, folderIDs []int64) (map[int64][]int64, error) {
	out := make(map[int64][]int64, len(folderIDs))
	if len(folderIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		FolderID, WorkID int64
	}
	err := s.db.WithContext(ctx).Raw(`
		SELECT f.id AS folder_id, r.work_id
		  FROM catalog_user_folder f
		  CROSS JOIN LATERAL (
		        SELECT i.work_id, i.created_at FROM catalog_user_folder_item i
		          JOIN catalog_work w ON w.id = i.work_id AND w.deleted_at IS NULL
		         WHERE i.folder_id = f.id
		         ORDER BY i.created_at DESC, i.work_id DESC LIMIT ?) r
		 WHERE f.id IN ?
		 ORDER BY f.id, r.created_at DESC, r.work_id DESC`,
		model.FolderRecentWorksMax, folderIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.FolderID] = append(out[r.FolderID], r.WorkID)
	}
	return out, nil
}

func (s *UserFolderService) publicFolders(ctx context.Context, q PublicFolderQuery) *gorm.DB {
	db := s.db.WithContext(ctx).Model(&model.CatalogUserFolder{}).
		Where("catalog_user_folder.visibility = ?", model.FolderVisibilityPublic)
	if q.OwnerUID > 0 {
		db = db.Where("catalog_user_folder.owner_uid = ?", q.OwnerUID)
	} else {
		db = db.Where("NOT catalog_user_folder.is_default AND btrim(catalog_user_folder.name) <> '' AND catalog_user_folder.item_count >= ?",
			model.FolderDirectoryMinItems)
		if !q.NSFW {
			db = db.Where("NOT " + folderHoldsRated(model.ContentRatingR18))
		}
	}
	if q.ContainsWorkID > 0 {
		db = db.Where(`EXISTS (SELECT 1 FROM catalog_user_folder_item ci JOIN catalog_work cw ON cw.id = ci.work_id
			WHERE ci.folder_id = catalog_user_folder.id AND ci.work_id = ? AND cw.deleted_at IS NULL)`, q.ContainsWorkID)
	}
	return db
}

func (s *UserFolderService) ListPublicFolders(ctx context.Context, q PublicFolderQuery) ([]model.CatalogUserFolder, error) {
	db := s.publicFolders(ctx, q)
	c := q.Cursor
	switch q.Sort {
	case FolderSortFollowers:
		if c.Set {
			db = db.Where("(catalog_user_folder.follower_count, catalog_user_folder.id) < (?, ?)", c.Followers, c.ID)
		}
		db = db.Order("catalog_user_folder.follower_count DESC, catalog_user_folder.id DESC")
	case FolderSortUpdated:
		if c.Set {
			db = db.Where("(catalog_user_folder.updated_at, catalog_user_folder.id) < (?, ?)", c.Updated, c.ID)
		}
		db = db.Order("catalog_user_folder.updated_at DESC, catalog_user_folder.id DESC")
	case FolderSortCreated:
		if c.Set {
			db = db.Where("catalog_user_folder.id < ?", c.ID)
		}
		db = db.Order("catalog_user_folder.id DESC")
	default:
		if c.Set {
			db = db.Where("catalog_user_folder.id > ?", c.ID)
		}
		db = db.Order("catalog_user_folder.id ASC")
	}
	var rows []model.CatalogUserFolder
	err := db.Limit(q.Limit).Find(&rows).Error
	return rows, err
}

func (s *UserFolderService) CountPublicFolders(ctx context.Context, q PublicFolderQuery) (int64, error) {
	var n int64
	err := s.publicFolders(ctx, q).Count(&n).Error
	return n, err
}

func (s *UserFolderService) FolderContentRatings(ctx context.Context, folderIDs []int64) (map[int64]int16, error) {
	out := make(map[int64]int16, len(folderIDs))
	if len(folderIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ID     int64
		Rating int16
	}
	err := s.db.WithContext(ctx).Model(&model.CatalogUserFolder{}).
		Select(fmt.Sprintf("catalog_user_folder.id AS id, CASE WHEN %s THEN %d WHEN %s THEN %d ELSE %d END AS rating",
			folderHoldsRated(model.ContentRatingR18), model.ContentRatingR18,
			folderHoldsRated(model.ContentRatingSensitive), model.ContentRatingSensitive,
			model.ContentRatingAllAges)).
		Where("catalog_user_folder.id IN ?", folderIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Rating
	}
	return out, nil
}

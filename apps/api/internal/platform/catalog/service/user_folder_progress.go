package service

import (
	"context"

	"api/internal/platform/catalog/model"
)

type FolderProgress struct {
	FolderID  int64
	ItemCount int
	Collected int
	Wish      int
	Doing     int
	Done      int
	OnHold    int
	Dropped   int
}

// Progress answers, for folders the bearer can read, how many of their live
// works the bearer keeps in another folder of their own and how many carry
// each play state. Folders the bearer cannot read are left out.
func (s *UserFolderService) Progress(ctx context.Context, uid int64, folderIDs []int64) ([]FolderProgress, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	if len(folderIDs) == 0 {
		return nil, nil
	}
	var rows []FolderProgress
	err := s.db.WithContext(ctx).Raw(`
		SELECT f.id AS folder_id,
		       count(i.work_id) AS item_count,
		       count(i.work_id) FILTER (WHERE EXISTS (
		           SELECT 1 FROM catalog_user_folder_item mi JOIN catalog_user_folder mf ON mf.id = mi.folder_id
		            WHERE mi.work_id = i.work_id AND mf.owner_uid = ? AND mf.id <> f.id)) AS collected,
		       count(i.work_id) FILTER (WHERE st.state = ?) AS wish,
		       count(i.work_id) FILTER (WHERE st.state = ?) AS doing,
		       count(i.work_id) FILTER (WHERE st.state = ?) AS done,
		       count(i.work_id) FILTER (WHERE st.state = ?) AS on_hold,
		       count(i.work_id) FILTER (WHERE st.state = ?) AS dropped
		  FROM catalog_user_folder f
		  LEFT JOIN (catalog_user_folder_item i JOIN catalog_work w ON w.id = i.work_id AND w.deleted_at IS NULL)
		         ON i.folder_id = f.id
		  LEFT JOIN catalog_user_work_state st ON st.actor_uid = ? AND st.work_id = i.work_id
		 WHERE f.id IN ? AND (f.`+readableFolder+` OR f.owner_uid = ?)
		 GROUP BY f.id
		 ORDER BY f.id`, uid,
		model.WorkStateWish, model.WorkStateDoing, model.WorkStateDone, model.WorkStateOnHold, model.WorkStateDropped,
		uid, folderIDs, uid).Scan(&rows).Error
	return rows, err
}

package service

import "context"

const recordedWorksSQL = `
	SELECT i.work_id FROM catalog_user_folder_item i
	  JOIN catalog_user_folder f ON f.id = i.folder_id
	 WHERE f.owner_uid = @uid AND i.work_id > @after
	UNION
	SELECT work_id FROM catalog_user_playtime WHERE actor_uid = @uid AND work_id > @after
	UNION
	SELECT work_id FROM catalog_user_work_state WHERE actor_uid = @uid AND work_id > @after`

func (s *UserFolderService) RecordedWorkIDs(ctx context.Context, uid, afterWorkID int64, limit int) ([]int64, error) {
	if uid <= 0 {
		return nil, ErrFolderActorRequired
	}
	var ids []int64
	err := s.db.WithContext(ctx).Raw(
		`SELECT work_id FROM (`+recordedWorksSQL+`) w ORDER BY work_id LIMIT @limit`,
		map[string]any{"uid": uid, "after": afterWorkID, "limit": limit},
	).Scan(&ids).Error
	return ids, err
}

func (s *UserFolderService) CountRecordedWorks(ctx context.Context, uid int64) (int64, error) {
	if uid <= 0 {
		return 0, ErrFolderActorRequired
	}
	var n int64
	err := s.db.WithContext(ctx).Raw(
		`SELECT count(*) FROM (`+recordedWorksSQL+`) w`,
		map[string]any{"uid": uid, "after": int64(0)},
	).Scan(&n).Error
	return n, err
}

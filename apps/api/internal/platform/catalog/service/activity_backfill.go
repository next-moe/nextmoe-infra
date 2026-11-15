package service

import (
	"context"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// The state tables keep only the latest state, so the one backfill there can
// be is each current state at its updated_at. Playtime has no per-day history
// at all and is not backfilled. A key the live write path already holds is
// left alone: it is newer than anything read here.
const playStateBackfillFrom = `
	  FROM catalog_user_work_state s
	  JOIN catalog_work w ON w.id = s.work_id AND w.status = @live
	  LEFT JOIN catalog_user_privacy p ON p.actor_uid = s.actor_uid
	 WHERE NOT COALESCE(p.play_records_private, false) AND s.state BETWEEN 1 AND 5`

const playStateBackfillName = `(CASE s.state WHEN 1 THEN 'wish' WHEN 2 THEN 'doing' WHEN 3 THEN 'done'
	    WHEN 4 THEN 'on_hold' ELSE 'dropped' END)`

func BackfillPlayStateFeed(ctx context.Context, db *gorm.DB, apply bool) (int64, error) {
	args := map[string]any{"live": model.WorkStatusLive, "rev": time.Now().UnixMicro(), "label": feedLabelWork}
	if !apply {
		var n int64
		err := db.WithContext(ctx).Raw(`SELECT count(*)`+playStateBackfillFrom+`
		   AND NOT EXISTS (SELECT 1 FROM catalog_activity_item a
		                    WHERE a.key = 'ws:' || s.actor_uid || ':' || s.work_id || ':' || `+playStateBackfillName+`)`, args).
			Scan(&n).Error
		return n, err
	}
	res := db.WithContext(ctx).Exec(`
		INSERT INTO catalog_activity_item (key, actor_uid, work_id, item, removed, revision, pushed_revision, updated_at)
		SELECT 'ws:' || s.actor_uid || ':' || s.work_id || ':' || `+playStateBackfillName+`, s.actor_uid, s.work_id,
		       jsonb_build_object(
		           'verb', 'mark', 'object_kind', 'work_' || `+playStateBackfillName+`, 'object_label', CAST(@label AS text),
		           'title', left(btrim(regexp_replace(w.display_name, '[[:space:][:cntrl:]]+', ' ', 'g')), 200),
		           'work_id', s.work_id, 'object_ref', 'work:' || s.work_id,
		           'content_limit', CASE WHEN w.content_rating = 0 THEN 'sfw' ELSE 'nsfw' END,
		           'occurred_at', s.updated_at),
		       false, CAST(@rev AS bigint), 0, now()`+playStateBackfillFrom+`
		ON CONFLICT (key) DO NOTHING`, args)
	return res.RowsAffected, res.Error
}

package docsync

import (
	"fmt"
	"log/slog"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// The galgame medium also holds every game the erogamescape, Bangumi and
// HowLongToBeat imports brought in, and on 2026-11-01 SEKIRO, Persona 5 and
// the whole Pokémon line sat in the rankings' top 300. A work counts as a
// visual novel when something that only lists visual novels or adult games
// vouches for it. Bangumi's ADV/AVG alone admitted Outer Wilds and The Last of
// Us; with an age tag beside it, it marks the novel-game listings.
const nonVNWorksSQL = `SELECT w.id FROM catalog_work w
	WHERE w.deleted_at IS NULL AND w.status = 0
		AND w.medium_id = (SELECT id FROM catalog_medium WHERE key = 'galgame')
		AND coalesce(w.site, '') = ''
		AND NOT EXISTS (SELECT 1 FROM catalog_external_ref e JOIN catalog_source src ON src.id = e.source_id
			WHERE e.entity_type = ? AND e.entity_id = w.id AND e.dead_at IS NULL AND src.key IN ('vndb', 'dlsite'))
		AND NOT EXISTS (SELECT 1 FROM catalog_release r
			JOIN catalog_external_ref e ON e.entity_type = ? AND e.entity_id = r.id AND e.dead_at IS NULL
			JOIN catalog_source src ON src.id = e.source_id AND src.key = 'dlsite'
			WHERE r.work_id = w.id)
		AND NOT EXISTS (SELECT 1 FROM catalog_external_ref e
			JOIN catalog_source src ON src.id = e.source_id AND src.key = 'bangumi'
			JOIN src_bangumi.subject sub ON sub.id::text = e.external_id
			WHERE e.entity_type = ? AND e.entity_id = w.id AND e.dead_at IS NULL AND (
				(jsonb_typeof(sub.meta_tags) = 'array' AND (
					jsonb_exists_any(sub.meta_tags, array['Galgame', 'galgame', 'VN', '乙女'])
					OR (jsonb_exists_any(sub.meta_tags, array['ADV', 'AVG']) AND jsonb_exists_any(sub.meta_tags, array['R18', '全年龄']))))
				OR EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(sub.tags) = 'array' THEN sub.tags ELSE '[]'::jsonb END) tg
					WHERE lower(tg->>'name') = ANY (array['galgame', '黄油', 'エロゲ', 'エロゲー', '美少女ゲーム', 'ギャルゲー', '视觉小说'])
						AND coalesce((tg->>'count')::int, 0) >= 3)))
		AND NOT (w.content_rating <> ? AND NOT EXISTS (SELECT 1 FROM catalog_external_ref e
			JOIN catalog_source src ON src.id = e.source_id AND src.key IN ('steam', 'howlongtobeat')
			WHERE e.entity_type = ? AND e.entity_id = w.id AND e.dead_at IS NULL))`

func loadNonVNWorks(db *gorm.DB, s scope) (map[int64]bool, error) {
	var mirrored bool
	if err := db.Raw(`SELECT to_regclass('src_bangumi.subject') IS NOT NULL`).Scan(&mirrored).Error; err != nil {
		return nil, fmt.Errorf("non-vn works: %w", err)
	}
	if !mirrored {
		slog.Warn("non-vn works skipped: no src_bangumi mirror, every work counts as a visual novel")
		return nil, nil
	}
	var ids []int64
	args := s.args(model.EntityTypeWork, model.EntityTypeRelease, model.EntityTypeWork,
		model.ContentRatingAllAges, model.EntityTypeWork)
	if err := db.Raw(nonVNWorksSQL+s.and("w.id"), args...).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("non-vn works: %w", err)
	}
	m := make(map[int64]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m, nil
}

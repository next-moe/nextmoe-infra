package tagcanon

import (
	"context"
	"log/slog"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type writer struct {
	db    *gorm.DB
	stats *Stats
}

func (w *writer) writeGroup(ctx context.Context, g group, apply bool) {
	if !apply {
		return
	}
	tagID, ok := w.ensureTag(ctx, g)
	if !ok {
		return
	}
	for _, m := range g.Members {
		res := w.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_name"}},
			DoNothing: true,
		}).Create(&model.CatalogTagSourceMap{SourceID: m.SourceID, SourceName: m.Name, TagID: tagID})
		if res.Error != nil {
			w.stats.Errors++
			slog.Warn("write tag map", "source", m.SourceID, "name", m.Name, "err", res.Error)
			continue
		}
		if res.RowsAffected == 0 {
			w.stats.MapsConflict++
			continue
		}
		w.stats.MapsCreated++
	}
}

// The editor can rename a tag (catalog.tag.name), after which the canonical
// name no longer finds it; resolving by name alone would mint a second tag
// under the old name. A member spelling that is already mapped names the tag.
func (w *writer) ensureTag(ctx context.Context, g group) (int64, bool) {
	if id, err := w.mappedTag(ctx, g); err != nil {
		w.stats.Errors++
		slog.Warn("resolve canonical tag by source map", "name", g.CanonicalName, "err", err)
		return 0, false
	} else if id > 0 {
		w.stats.TagsConflict++
		return id, true
	}
	tag := model.CatalogTag{Name: g.CanonicalName, Tier: g.Tier, Kind: g.Kind, Sexual: g.Sexual}
	res := w.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoNothing: true,
	}).Create(&tag)
	if res.Error != nil {
		w.stats.Errors++
		slog.Warn("write canonical tag", "name", g.CanonicalName, "err", res.Error)
		return 0, false
	}
	if res.RowsAffected == 1 {
		w.stats.TagsCreated++
		return tag.ID, true
	}
	w.stats.TagsConflict++
	var id int64
	if err := w.db.WithContext(ctx).Raw(`SELECT id FROM catalog_tag WHERE name = ?`, g.CanonicalName).Scan(&id).Error; err != nil || id == 0 {
		w.stats.Errors++
		slog.Warn("resolve existing canonical tag id", "name", g.CanonicalName, "err", err)
		return 0, false
	}
	return id, true
}

func (w *writer) mappedTag(ctx context.Context, g group) (int64, error) {
	if len(g.Members) == 0 {
		return 0, nil
	}
	values := make([]string, 0, len(g.Members))
	args := make([]any, 0, 2*len(g.Members))
	for _, m := range g.Members {
		values = append(values, "(?::smallint, ?::text)")
		args = append(args, m.SourceID, m.Name)
	}
	var ids []int64
	err := w.db.WithContext(ctx).Raw(`SELECT tag_id FROM catalog_tag_source_map
		WHERE (source_id, source_name) IN (VALUES `+strings.Join(values, ", ")+`)
		GROUP BY tag_id ORDER BY count(*) DESC, tag_id LIMIT 1`, args...).Scan(&ids).Error
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	return ids[0], nil
}

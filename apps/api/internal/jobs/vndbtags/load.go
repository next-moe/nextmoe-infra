package vndbtags

import (
	"context"
	"fmt"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

func loadTagMeta(ctx context.Context, db *gorm.DB) (map[string]TagMeta, error) {
	var rows []struct {
		ID           string `gorm:"column:id"`
		Cat          string `gorm:"column:cat"`
		DefaultSpoil int16  `gorm:"column:defaultspoil"`
		Name         string `gorm:"column:name"`
		Alias        string `gorm:"column:alias"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT id, cat, defaultspoil, name, coalesce(alias, '') AS alias FROM src_vndb.tags`,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load src_vndb.tags: %w", err)
	}
	out := make(map[string]TagMeta, len(rows))
	for _, r := range rows {
		out[r.ID] = TagMeta{Name: r.Name, Alias: r.Alias, Cat: r.Cat, DefaultSpoil: r.DefaultSpoil}
	}
	return out, nil
}

func loadSexualByName(ctx context.Context, db *gorm.DB, vndbID int16) (map[string]bool, error) {
	var rows []struct {
		Name   string `gorm:"column:name"`
		Sexual bool   `gorm:"column:sexual"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT name, bool_or(sexual) AS sexual FROM catalog_work_tag WHERE source_id = ? GROUP BY name`,
		vndbID,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load vndb tag sexual: %w", err)
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.Name] = r.Sexual
	}
	return out, nil
}

func loadVotes(ctx context.Context, db *gorm.DB, vids []string) ([]Vote, error) {
	if len(vids) == 0 {
		return nil, nil
	}
	var rows []Vote
	if err := db.WithContext(ctx).Raw(
		`SELECT tag, vid, vote AS score, spoiler, ignore, lie FROM src_vndb.tags_vn WHERE vid IN ?`,
		vids,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load src_vndb.tags_vn: %w", err)
	}
	return rows, nil
}

func loadExisting(ctx context.Context, db *gorm.DB, vndbID int16, workIDs []int64) (map[int64][]Existing, error) {
	out := map[int64][]Existing{}
	if len(workIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ID      int64  `gorm:"column:id"`
		WorkID  int64  `gorm:"column:work_id"`
		Name    string `gorm:"column:name"`
		Spoiler int16  `gorm:"column:spoiler"`
		Count   int    `gorm:"column:count"`
		Sexual  bool   `gorm:"column:sexual"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT id, work_id, name, spoiler, count, sexual FROM catalog_work_tag WHERE source_id = ? AND work_id IN ?`,
		vndbID, workIDs,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load catalog_work_tag: %w", err)
	}
	for _, r := range rows {
		out[r.WorkID] = append(out[r.WorkID], Existing{
			ID: r.ID, Name: r.Name, Spoiler: r.Spoiler, Count: r.Count, Sexual: r.Sexual,
		})
	}
	return out, nil
}

func loadOrphans(ctx context.Context, db *gorm.DB, vndbID int16) ([]orphanWork, error) {
	var rows []struct {
		WorkID  int64  `gorm:"column:work_id"`
		ID      int64  `gorm:"column:id"`
		Name    string `gorm:"column:name"`
		Spoiler int16  `gorm:"column:spoiler"`
		Count   int    `gorm:"column:count"`
		Sexual  bool   `gorm:"column:sexual"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT t.work_id, t.id, t.name, t.spoiler, t.count, t.sexual
		FROM catalog_work_tag t
		JOIN catalog_work w ON w.id = t.work_id
		WHERE t.source_id = ?
		  AND w.deleted_at IS NULL AND w.status <> ?
		  AND NOT EXISTS (
		    SELECT 1 FROM catalog_external_ref r
		    WHERE r.entity_id = w.id
		      AND r.entity_type = ?
		      AND r.source_id = ?
		      AND r.link_kind = ?
		      AND r.dead_at IS NULL
		  )
		ORDER BY t.work_id, t.id`,
		vndbID, model.WorkStatusMerged,
		model.EntityTypeWork, vndbID, model.LinkKindExact,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load orphan vndb tags: %w", err)
	}
	var out []orphanWork
	i := 0
	for i < len(rows) {
		id := rows[i].WorkID
		ow := orphanWork{WorkID: id}
		for i < len(rows) && rows[i].WorkID == id {
			ow.Rows = append(ow.Rows, Existing{
				ID: rows[i].ID, Name: rows[i].Name,
				Spoiler: rows[i].Spoiler, Count: rows[i].Count, Sexual: rows[i].Sexual,
			})
			i++
		}
		out = append(out, ow)
	}
	return out, nil
}

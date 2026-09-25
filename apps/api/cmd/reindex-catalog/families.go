package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	catalogSearch "api/internal/platform/catalog/search"

	"gorm.io/gorm"
)

func loadGroupedCount(db *gorm.DB, table, col string) (map[int64]int, error) {
	var rows []struct {
		ID  int64 `gorm:"column:id"`
		Cnt int   `gorm:"column:cnt"`
	}
	q := fmt.Sprintf(`SELECT %s AS id, count(*) AS cnt FROM %s GROUP BY %s`, col, table, col)
	if err := db.Raw(q).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int, len(rows))
	for _, r := range rows {
		m[r.ID] = r.Cnt
	}
	return m, nil
}

func reindexSeries(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, batch int) error {
	pop, err := loadGroupedCount(db, "catalog_series_member", "series_id")
	if err != nil {
		return err
	}
	processed, lastID := 0, int64(0)
	for {
		var rows []struct {
			ID          int64  `gorm:"column:id"`
			DisplayName string `gorm:"column:display_name"`
			SourceKey   string `gorm:"column:source_key"`
			ExternalID  string `gorm:"column:external_id"`
			NameZh      string `gorm:"column:name_zh"`
		}
		if err := db.Raw(`SELECT s.id, s.display_name, src.key AS source_key, s.external_id,
				coalesce(o.display_name, '') AS name_zh
			FROM catalog_series s
			JOIN catalog_source src ON src.id = s.source_id
			LEFT JOIN catalog_series_name_override o
				ON o.source_id = s.source_id AND o.external_id = s.external_id
			WHERE s.id > ? ORDER BY s.id LIMIT ?`, lastID, batch).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		docs := make([]catalogSearch.EntityDoc, len(rows))
		for i, r := range rows {
			d := catalogSearch.EntityDoc{
				ID: "s" + fmt.Sprint(r.ID), EntityType: "series",
				NameOther: r.DisplayName, NameZh: r.NameZh,
				Sources: []string{r.SourceKey + ":" + r.ExternalID}, SourceKeys: []string{r.SourceKey},
				Popularity: catalogSearch.Popularity(pop[r.ID]),
			}
			docs[i] = d
		}
		if err := idx.UpsertBatch(ctx, catalogSearch.IndexSeries, docs); err != nil {
			return err
		}
		processed += len(rows)
		lastID = rows[len(rows)-1].ID
	}
	slog.Info("reindexed", "index", catalogSearch.IndexSeries, "docs", processed)
	return nil
}

func reindexEngines(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, batch int) error {
	pop, err := loadGroupedCount(db, "catalog_work_engine", "engine_id")
	if err != nil {
		return err
	}
	processed, lastID := 0, int64(0)
	for {
		var rows []struct {
			ID      int64  `gorm:"column:id"`
			Name    string `gorm:"column:name"`
			Aliases []byte `gorm:"column:aliases"`
		}
		if err := db.Raw(`SELECT id, name, aliases FROM catalog_engine WHERE id > ? ORDER BY id LIMIT ?`,
			lastID, batch).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		docs := make([]catalogSearch.EntityDoc, len(rows))
		for i, r := range rows {
			docs[i] = catalogSearch.EntityDoc{
				ID: "e" + fmt.Sprint(r.ID), EntityType: "engine",
				NameOther: r.Name, AliasesOther: engineAliasList(r.Aliases),
				Popularity: catalogSearch.Popularity(pop[r.ID]),
			}
		}
		if err := idx.UpsertBatch(ctx, catalogSearch.IndexEngines, docs); err != nil {
			return err
		}
		processed += len(rows)
		lastID = rows[len(rows)-1].ID
	}
	slog.Info("reindexed", "index", catalogSearch.IndexEngines, "docs", processed)
	return nil
}

func reindexTraits(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, batch int) error {
	pop, err := loadGroupedCount(db, "catalog_character_trait_link", "trait_id")
	if err != nil {
		return err
	}
	processed, lastID := 0, int64(0)
	for {
		var rows []struct {
			ID      int64  `gorm:"column:id"`
			VndbTID string `gorm:"column:vndb_tid"`
			Name    string `gorm:"column:name"`
			NameZh  string `gorm:"column:name_zh"`
			Alias   string `gorm:"column:alias"`
			Sexual  bool   `gorm:"column:sexual"`
		}
		if err := db.Raw(`SELECT id, vndb_tid, name, name_zh, alias, sexual_family AS sexual
			FROM catalog_character_trait WHERE id > ? AND searchable = true ORDER BY id LIMIT ?`,
			lastID, batch).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		docs := make([]catalogSearch.EntityDoc, len(rows))
		for i, r := range rows {
			sexual := r.Sexual
			docs[i] = catalogSearch.EntityDoc{
				ID: "f" + fmt.Sprint(r.ID), EntityType: "trait",
				NameOther: r.Name, NameZh: r.NameZh, AliasesOther: splitTraitAliases(r.Alias),
				Sources: []string{"vndb:" + r.VndbTID}, SourceKeys: []string{"vndb"},
				Sexual: &sexual, Popularity: catalogSearch.Popularity(pop[r.ID]),
			}
		}
		if err := idx.UpsertBatch(ctx, catalogSearch.IndexTraits, docs); err != nil {
			return err
		}
		processed += len(rows)
		lastID = rows[len(rows)-1].ID
	}
	slog.Info("reindexed", "index", catalogSearch.IndexTraits, "docs", processed)
	return nil
}

func engineAliasList(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var vals []string
	if err := json.Unmarshal(raw, &vals); err != nil {
		return nil
	}
	var out []string
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func splitTraitAliases(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for line := range strings.SplitSeq(raw, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

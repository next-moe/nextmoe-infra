package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"time"

	"api/internal/infrastructure/database"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	catalogSearch "api/internal/platform/catalog/search"
	"api/pkg/config"
	"api/pkg/logger"

	"gorm.io/gorm"
)

func main() {
	indexFlag := flag.String("index", "catalog_credit_names,catalog_characters,catalog_labels,catalog_works,catalog_tags,catalog_series,catalog_engines,catalog_traits", "comma-separated index uids")
	batch := flag.Int("batch", 5000, "batch size per search upsert")
	recreate := flag.Bool("recreate", false, "drop and recreate each named index (opensearch only)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
	logger.Init(cfg.Server.Env)

	db, err := database.NewPostgresDB(cfg.CatalogDatabase)
	if err != nil {
		slog.Error("catalog db connect", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	idx, engineName, err := catalogSearch.NewIndexerFromConfig(cfg)
	if err != nil {
		slog.Error("search indexer", "error", err)
		os.Exit(1)
	}
	slog.Info("reindex-catalog engine", "engine", engineName)
	if *recreate && engineName != catalogSearch.EngineOpenSearch {
		slog.Error("-recreate is unsupported for this search engine", "engine", engineName)
		os.Exit(1)
	}
	ctx := context.Background()
	if err := idx.Health(ctx); err != nil {
		slog.Error("search engine unreachable", "engine", engineName, "error", err)
		os.Exit(1)
	}
	indexes := strings.Split(*indexFlag, ",")
	if *recreate {
		for _, t := range indexes {
			t = strings.TrimSpace(t)
			switch t {
			case catalogSearch.IndexCreditNames, catalogSearch.IndexCharacters, catalogSearch.IndexLabels, catalogSearch.IndexWorks, catalogSearch.IndexTags, catalogSearch.IndexSeries, catalogSearch.IndexEngines, catalogSearch.IndexTraits:
				if err := idx.RecreateIndex(ctx, t); err != nil {
					slog.Error("recreate failed", "index", t, "error", err)
					os.Exit(1)
				}
			case "":
				continue
			default:
				slog.Warn("unknown index, skipping", "index", t)
			}
		}
	}
	if err := idx.EnsureIndexes(ctx); err != nil {
		slog.Error("ensure indexes", "error", err)
		os.Exit(1)
	}

	start := time.Now()
	for _, t := range indexes {
		t = strings.TrimSpace(t)
		var err error
		switch t {
		case catalogSearch.IndexCreditNames:
			err = reindexCreditNames(ctx, db.DB(), idx, *batch)
		case catalogSearch.IndexCharacters:
			err = reindexEntity(ctx, db.DB(), idx, *batch, catalogSearch.IndexCharacters, "catalog_character", "c",
				model.EntityTypeCharacter, "character_id", "character", "catalog_character_alias", "character_id",
				editspec.NotSuppressedCharacterAliasSQL("a"))
		case catalogSearch.IndexLabels:
			err = reindexLabels(ctx, db.DB(), idx, *batch)
		case catalogSearch.IndexWorks:
			err = reindexWorks(ctx, db.DB(), idx, *batch)
		case catalogSearch.IndexTags:
			err = reindexTags(ctx, db.DB(), idx, *batch)
		case catalogSearch.IndexSeries:
			err = reindexSeries(ctx, db.DB(), idx, *batch)
		case catalogSearch.IndexEngines:
			err = reindexEngines(ctx, db.DB(), idx, *batch)
		case catalogSearch.IndexTraits:
			err = reindexTraits(ctx, db.DB(), idx, *batch)
		case "":
			continue
		default:
			slog.Warn("unknown index, skipping", "index", t)
			continue
		}
		if err != nil {
			slog.Error("reindex failed", "index", t, "error", err)
			os.Exit(1)
		}
		if err := idx.Refresh(ctx, t); err != nil {
			slog.Error("refresh failed", "index", t, "error", err)
			os.Exit(1)
		}
	}
	slog.Info("reindex-catalog complete", "duration", time.Since(start))
}

func loadPopularity(db *gorm.DB, col string) (map[int64]int, error) {
	var rows []struct {
		ID  int64 `gorm:"column:id"`
		Cnt int   `gorm:"column:cnt"`
	}
	where := ""
	if col != "credit_name_id" {
		where = "WHERE " + col + " IS NOT NULL"
	}
	if err := db.Raw(fmt.Sprintf(`SELECT %s AS id, count(*) AS cnt FROM catalog_credit %s GROUP BY %s`, col, where, col)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int, len(rows))
	for _, r := range rows {
		m[r.ID] = r.Cnt
	}
	return m, nil
}

func loadSources(db *gorm.DB, entityType int16) (map[int64][]string, map[int64][]string, error) {
	var rows []struct {
		EntityID int64  `gorm:"column:entity_id"`
		Key      string `gorm:"column:key"`
		Ext      string `gorm:"column:external_id"`
	}
	if err := db.Raw(`SELECT r.entity_id, s.key, r.external_id FROM catalog_external_ref r
		JOIN catalog_source s ON s.id = r.source_id WHERE r.entity_type = ? ORDER BY r.entity_id`, entityType).Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	srcs := map[int64][]string{}
	keys := map[int64][]string{}
	keySeen := map[int64]map[string]bool{}
	for _, r := range rows {
		srcs[r.EntityID] = append(srcs[r.EntityID], r.Key+":"+r.Ext)
		if keySeen[r.EntityID] == nil {
			keySeen[r.EntityID] = map[string]bool{}
		}
		if !keySeen[r.EntityID][r.Key] {
			keySeen[r.EntityID][r.Key] = true
			keys[r.EntityID] = append(keys[r.EntityID], r.Key)
		}
	}
	return srcs, keys, nil
}

func reindexCreditNames(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, batch int) error {
	pop, err := loadPopularity(db, "credit_name_id")
	if err != nil {
		return err
	}
	srcs, keys, err := loadSources(db, model.EntityTypeCreditName)
	if err != nil {
		return err
	}
	aliases, err := loadAliases(db)
	if err != nil {
		return err
	}
	processed, lastID := 0, int64(0)
	for {
		var rows []struct {
			ID       int64  `gorm:"column:id"`
			Name     string `gorm:"column:name"`
			Lang     string `gorm:"column:lang"`
			Latin    string `gorm:"column:latin"`
			PersonID *int64 `gorm:"column:person_id"`
		}
		if err := db.Raw(`SELECT id, name, lang, coalesce(latin,'') AS latin, person_id FROM catalog_credit_name WHERE id > ? ORDER BY id LIMIT ?`, lastID, batch).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		docs := make([]catalogSearch.EntityDoc, len(rows))
		for i, r := range rows {
			d := catalogSearch.EntityDoc{
				ID: "n" + fmt.Sprint(r.ID), EntityType: "credit_name", Latin: r.Latin,
				Sources: srcs[r.ID], SourceKeys: keys[r.ID], PersonID: r.PersonID,
				Popularity: catalogSearch.Popularity(pop[r.ID]),
			}
			d.SetName(r.Lang, r.Name)
			for _, a := range aliases[r.ID] {
				d.AddAlias(a.lang, a.name)
			}
			docs[i] = d
		}
		if err := idx.UpsertBatch(ctx, catalogSearch.IndexCreditNames, docs); err != nil {
			return err
		}
		processed += len(rows)
		lastID = rows[len(rows)-1].ID
	}
	slog.Info("reindexed", "index", catalogSearch.IndexCreditNames, "docs", processed)
	return nil
}

func reindexLabels(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, batch int) error {
	pop, err := loadPopularity(db, "label_id")
	if err != nil {
		return err
	}
	srcs, keys, err := loadSources(db, model.EntityTypeLabel)
	if err != nil {
		return err
	}
	aliases, err := loadAliasTable(db, "catalog_label_alias", "label_id", "")
	if err != nil {
		return err
	}
	if err := purgeSoftDeleted(ctx, db, idx, catalogSearch.IndexLabels, "catalog_label", "b"); err != nil {
		return err
	}
	processed, lastID := 0, int64(0)
	for {
		var rows []struct {
			ID    int64  `gorm:"column:id"`
			Name  string `gorm:"column:display_name"`
			Lang  string `gorm:"column:lang"`
			Latin string `gorm:"column:latin"`
			Kind  int16  `gorm:"column:kind"`
		}
		if err := db.Raw(`SELECT id, display_name, lang, coalesce(latin,'') AS latin, kind FROM catalog_label
			WHERE id > ? AND deleted_at IS NULL ORDER BY id LIMIT ?`, lastID, batch).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		docs := make([]catalogSearch.EntityDoc, len(rows))
		for i, r := range rows {
			kind := r.Kind
			d := catalogSearch.EntityDoc{
				ID: "b" + fmt.Sprint(r.ID), EntityType: "label", Latin: r.Latin,
				Sources: srcs[r.ID], SourceKeys: keys[r.ID], Kind: &kind,
				Popularity: catalogSearch.Popularity(pop[r.ID]),
			}
			d.SetName(r.Lang, r.Name)
			for _, a := range aliases[r.ID] {
				d.AddAlias(a.lang, a.name)
			}
			docs[i] = d
		}
		if err := idx.UpsertBatch(ctx, catalogSearch.IndexLabels, docs); err != nil {
			return err
		}
		processed += len(rows)
		lastID = rows[len(rows)-1].ID
	}
	slog.Info("reindexed", "index", catalogSearch.IndexLabels, "docs", processed)
	return nil
}

func reindexEntity(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, batch int, uid, table, prefix string, entityType int16, popCol, etype, aliasTable, aliasCol, aliasLive string) error {
	pop, err := loadPopularity(db, popCol)
	if err != nil {
		return err
	}
	srcs, keys, err := loadSources(db, entityType)
	if err != nil {
		return err
	}
	aliases, err := loadAliasTable(db, aliasTable, aliasCol, aliasLive)
	if err != nil {
		return err
	}
	if err := purgeSoftDeleted(ctx, db, idx, uid, table, prefix); err != nil {
		return err
	}
	processed, lastID := 0, int64(0)
	for {
		var rows []struct {
			ID    int64  `gorm:"column:id"`
			Name  string `gorm:"column:display_name"`
			Lang  string `gorm:"column:lang"`
			Latin string `gorm:"column:latin"`
		}
		if err := db.Raw(fmt.Sprintf(`SELECT id, display_name, lang, coalesce(latin,'') AS latin FROM %s
			WHERE id > ? AND deleted_at IS NULL ORDER BY id LIMIT ?`, table), lastID, batch).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		docs := make([]catalogSearch.EntityDoc, len(rows))
		for i, r := range rows {
			d := catalogSearch.EntityDoc{
				ID: prefix + fmt.Sprint(r.ID), EntityType: etype, Latin: r.Latin,
				Sources: srcs[r.ID], SourceKeys: keys[r.ID], Popularity: catalogSearch.Popularity(pop[r.ID]),
			}
			d.SetName(r.Lang, r.Name)
			for _, a := range aliases[r.ID] {
				d.AddAlias(a.lang, a.name)
			}
			docs[i] = d
		}
		if err := idx.UpsertBatch(ctx, uid, docs); err != nil {
			return err
		}
		processed += len(rows)
		lastID = rows[len(rows)-1].ID
	}
	slog.Info("reindexed", "index", uid, "docs", processed)
	return nil
}

type alias struct{ lang, name string }

func loadAliases(db *gorm.DB) (map[int64][]alias, error) {
	return loadAliasTable(db, "catalog_name_alias", "credit_name_id", "")
}

func loadAliasTable(db *gorm.DB, table, ownerCol, live string) (map[int64][]alias, error) {
	var rows []struct {
		OwnerID int64  `gorm:"column:owner_id"`
		Name    string `gorm:"column:name"`
		Lang    string `gorm:"column:lang"`
	}
	q := fmt.Sprintf(`SELECT a.%s AS owner_id, a.name, a.lang FROM %s a`, ownerCol, table)
	if live != "" {
		q += " WHERE " + live
	}
	if err := db.Raw(q).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := map[int64][]alias{}
	for _, r := range rows {
		m[r.OwnerID] = append(m[r.OwnerID], alias{lang: r.Lang, name: r.Name})
	}
	return m, nil
}

func purgeSoftDeleted(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, uid, table, prefix string) error {
	var ids []int64
	if err := db.Raw(fmt.Sprintf(
		`SELECT id FROM %s WHERE deleted_at IS NOT NULL ORDER BY id`, table)).Scan(&ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	docIDs := make([]string, len(ids))
	for i, id := range ids {
		docIDs[i] = prefix + fmt.Sprint(id)
	}
	if err := idx.DeleteBatch(ctx, uid, docIDs); err != nil {
		return err
	}
	slog.Info("purged soft-deleted documents", "index", uid, "docs", len(docIDs))
	return nil
}

func loadWorkPopularitySignal(db *gorm.DB) (map[int64]float64, error) {
	var rows []struct {
		WorkID int64 `gorm:"column:work_id"`
		V      int64 `gorm:"column:v"`
	}
	if err := db.Raw(`SELECT work_id, max(value) AS v FROM catalog_work_popularity
		WHERE metric IN (?, ?) GROUP BY work_id`,
		model.PopularityMetricBgmCollect, model.PopularityMetricDownloads).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]float64, len(rows))
	for _, r := range rows {
		m[r.WorkID] = math.Log1p(float64(r.V))
	}
	return m, nil
}

type workTitle struct {
	lang, title, latin string
	kind               int16
}

func loadWorkTitles(db *gorm.DB) (map[int64][]workTitle, error) {
	const population = `w.deleted_at IS NULL AND w.status = 0
		AND w.medium_id = (SELECT id FROM catalog_medium WHERE key = 'galgame')`

	m := map[int64][]workTitle{}
	var native []struct {
		WorkID int64  `gorm:"column:work_id"`
		Lang   string `gorm:"column:lang"`
		Title  string `gorm:"column:title"`
		Latin  string `gorm:"column:latin"`
		Kind   int16  `gorm:"column:kind"`
	}
	if err := db.Raw(`SELECT t.work_id, t.lang, t.title, coalesce(t.latin,'') AS latin, t.kind
		FROM catalog_work_title t
		JOIN catalog_work w ON w.id = t.work_id
		WHERE ` + population + ` AND ` + editspec.NotSuppressedWorkTitleSQL("t") + `
		ORDER BY t.work_id, t.kind, t.lang, t.id`).Scan(&native).Error; err != nil {
		return nil, err
	}
	for _, r := range native {
		m[r.WorkID] = append(m[r.WorkID], workTitle{lang: r.Lang, title: r.Title, latin: r.Latin, kind: r.Kind})
	}
	return m, nil
}

type workIntro struct{ lang, text string }

func loadWorkIntros(db *gorm.DB) (map[int64][]workIntro, error) {
	const population = `w.deleted_at IS NULL AND w.status = 0
		AND w.medium_id = (SELECT id FROM catalog_medium WHERE key = 'galgame')`

	out := map[int64][]workIntro{}
	var native []struct {
		WorkID int64  `gorm:"column:work_id"`
		Lang   string `gorm:"column:lang"`
		Intro  string `gorm:"column:intro"`
	}
	if err := db.Raw(`SELECT i.work_id, i.lang, i.intro
		FROM catalog_work_intro i JOIN catalog_work w ON w.id = i.work_id
		WHERE ` + population + `
		ORDER BY i.work_id, i.lang, i.provenance, ` +
		editspec.HumanLaneFirstSQL("i.source_id", "i.provenance") +
		`, i.source_id, i.id`).Scan(&native).Error; err != nil {
		return nil, err
	}
	seen := map[int64]map[string]bool{}
	for _, r := range native {
		langs := seen[r.WorkID]
		if langs == nil {
			langs = map[string]bool{}
			seen[r.WorkID] = langs
		}
		if langs[r.Lang] {
			continue
		}
		langs[r.Lang] = true
		out[r.WorkID] = append(out[r.WorkID], workIntro{lang: r.Lang, text: r.Intro})
	}
	return out, nil
}

func reindexWorks(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, batch int) error {
	pop, err := loadWorkPopularitySignal(db)
	if err != nil {
		return err
	}
	srcs, keys, err := loadSources(db, model.EntityTypeWork)
	if err != nil {
		return err
	}
	titles, err := loadWorkTitles(db)
	if err != nil {
		return err
	}
	facets, err := loadWorksFacets(db)
	if err != nil {
		return err
	}
	intros, err := loadWorkIntros(db)
	if err != nil {
		return err
	}
	// Works was the only soft-deleting lane without this purge: the 2026-08-29
	// merge wave left its 3,242 soft-deleted works in the index — hits
	// self-healed through DB hydration, totals and ranking slots did not — and
	// the stale documents had to be hand-deleted over the search API.
	if err := purgeSoftDeleted(ctx, db, idx, catalogSearch.IndexWorks, "catalog_work", "w"); err != nil {
		return err
	}
	processed, lastID := 0, int64(0)
	for {
		var rows []struct {
			ID          int64  `gorm:"column:id"`
			DisplayName string `gorm:"column:display_name"`
			// Explicit column tag: GORM snake-cases OLang to o_lang, which
			// matches no result column and would scan as "" — the trap that left
			// the works list's olang empty from W1 until A2-1a.
			OLang         string    `gorm:"column:olang"`
			ContentRating int16     `gorm:"column:content_rating"`
			Site          string    `gorm:"column:site"`
			ProductWorkID *int64    `gorm:"column:product_work_id"`
			ClaimState    *int16    `gorm:"column:claim_state"`
			UpdatedAt     time.Time `gorm:"column:updated_at"`
			DisplayNSFW   bool      `gorm:"column:display_nsfw"`
			// cover_art_all_explicit is trigger-maintained; content_limit reaches
			// the index only through this job, so omitting it here would serve a
			// stale shelf on the search face until the next daily run.
			CoverArtAllExplicit bool `gorm:"column:cover_art_all_explicit"`
		}
		if err := db.Raw(`SELECT w.id, w.display_name, w.olang, w.content_rating, coalesce(w.site,'') AS site,
				w.product_work_id, w.claim_state, w.updated_at, w.display_nsfw, w.cover_art_all_explicit
			FROM catalog_work w
			WHERE w.id > ? AND w.deleted_at IS NULL AND w.status = 0
				AND w.medium_id = (SELECT id FROM catalog_medium WHERE key = 'galgame')
			ORDER BY w.id LIMIT ?`, lastID, batch).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		docs := make([]catalogSearch.EntityDoc, len(rows))
		for i, r := range rows {
			in := catalogSearch.WorkDocInput{
				ID: r.ID, DisplayName: r.DisplayName, OLang: r.OLang,
				ContentRating: r.ContentRating,
				Claimed:       r.Site != "",
				ClaimState:    model.ClaimStateKey(&r.Site, r.ProductWorkID, r.ClaimState),
				ContentLimit: model.DisplayLimitKey(model.WorkShelf{
					Site: &r.Site, ProductWorkID: r.ProductWorkID, DisplayNSFW: r.DisplayNSFW,
					ContentRating: r.ContentRating, CoverArtAllExplicit: r.CoverArtAllExplicit,
				}),
				ReleasedOrd: facets.releasedOrd[r.ID], UpdatedTS: r.UpdatedAt.Unix(),
				Popularity: pop[r.ID], Sources: srcs[r.ID], SourceKeys: keys[r.ID],
				TagIDs: facets.tagIDs[r.ID], LabelIDs: facets.labelIDs[r.ID],
				EngineIDs: facets.engineIDs[r.ID], SeriesIDs: facets.seriesIDs[r.ID],
			}
			for _, t := range titles[r.ID] {
				in.Titles = append(in.Titles, catalogSearch.WorkDocTitle{Lang: t.lang, Title: t.title, Latin: t.latin})
			}
			for _, iv := range intros[r.ID] {
				in.Intros = append(in.Intros, catalogSearch.WorkDocIntro{Lang: iv.lang, Text: iv.text})
			}
			docs[i] = catalogSearch.BuildWorkDoc(in)
		}
		if err := idx.UpsertBatch(ctx, catalogSearch.IndexWorks, docs); err != nil {
			return err
		}
		processed += len(rows)
		lastID = rows[len(rows)-1].ID
	}
	slog.Info("reindexed", "index", catalogSearch.IndexWorks, "docs", processed)
	return nil
}

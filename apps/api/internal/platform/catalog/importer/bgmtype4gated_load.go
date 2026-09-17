package importer

import (
	"fmt"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

func buildBgmGatedPoolQuery() string {
	metaArr := `jsonb_typeof(s.meta_tags)='array'`
	exAny := func(arr string) string { return `jsonb_exists_any(s.meta_tags, ` + arr + `)` }
	ex := func(k string) string { return `jsonb_exists(s.meta_tags, '` + k + `')` }
	sigP := metaArr + ` AND ` + exAny(sqlPCPlatforms) + ` AND (` +
		exAny(sqlGenreStrict) + ` OR (` + exAny(sqlGenreAdv) + ` AND ` + exAny(sqlAgeRated) + `))`
	sigT := `(` + metaArr + ` AND (` + ex(`Galgame`) + ` OR ` + ex(`galgame`) + `))` +
		` OR EXISTS(SELECT 1 FROM jsonb_array_elements(s.tags) tg WHERE jsonb_typeof(s.tags)='array'` +
		` AND lower(tg->>'name') = ANY(` + sqlGalgameFolk + `) AND coalesce((tg->>'count')::int,0) >= 3)`
	excluded := metaArr + ` AND ` + exAny(sqlConsoleMobile) + ` AND NOT ` + exAny(sqlPCFamily)
	return `SELECT s.id AS id, coalesce(s.name,'') AS name, coalesce(s.name_cn,'') AS name_cn,
			coalesce(s.name_norm,'') AS name_norm, coalesce(s.name_cn_norm,'') AS name_cn_norm, s.nsfw AS nsfw,
			(` + sigP + `) AS sig_p, (` + sigT + `) AS sig_t, (` + excluded + `) AS excluded
		FROM src_bangumi.subject s
		WHERE s.type = 4
			AND NOT EXISTS (SELECT 1 FROM catalog_external_ref e
				WHERE e.source_id = ? AND e.entity_type = ? AND e.external_id = s.id::text)
		ORDER BY s.id`
}

func (im *Importer) loadGatedPool() ([]poolRow, error) {
	var rows []poolRow
	err := im.catalog.Raw(buildBgmGatedPoolQuery(), bangumiSource, model.EntityTypeWork).Scan(&rows).Error
	return rows, err
}

// loadExistingWorkTitleNorms keys the collision corpus on SPACE-FOLDED norms
// and carries display_name as well as the title rows. The 2026-07 gated wave
// minted ~4.9k duplicates through exactly those two holes: wiki-claimed works
// were born with no catalog_work_title rows at all, and a title differing only
// by an internal space compared unequal.
func (im *Importer) loadExistingWorkTitleNorms() (map[string]wtNorm, error) {
	type normRow struct {
		Norm   string `gorm:"column:title_norm"`
		WorkID int64  `gorm:"column:work_id"`
		Title  string `gorm:"column:title"`
	}
	out := make(map[string]wtNorm)
	collect := func(query string, args ...any) error {
		var rows []normRow
		if err := im.catalog.Raw(query, args...).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			keys := []string{foldSpace(r.Norm), looseGateKey(r.Norm)}
			for _, key := range keys {
				if !service.WorkDupeNormEligible(key) {
					continue
				}
				if _, ok := out[key]; !ok {
					out[key] = wtNorm{workID: r.WorkID, title: r.Title}
				}
			}
		}
		return nil
	}
	if err := collect(
		`SELECT title_norm, work_id, title FROM catalog_work_title t
		 JOIN catalog_work w ON w.id = t.work_id AND w.deleted_at IS NULL
		 WHERE ` + service.WorkDupeNormEligibleSQL("title_norm") + ` AND ` + editspec.NotSuppressedWorkTitleSQL("t"),
	); err != nil {
		return nil, err
	}
	if err := collect(
		`SELECT lower(normalize(w.display_name, NFKC)) AS title_norm, w.id AS work_id, w.display_name AS title
		 FROM catalog_work w
		 WHERE w.deleted_at IS NULL AND ` + service.WorkDupeNormEligibleSQL("w.display_name"),
	); err != nil {
		return nil, err
	}
	return out, nil
}

func (im *Importer) loadCrossSourceNorms(dlsiteDB *gorm.DB) (map[string]uint8, error) {
	set := make(map[string]uint8, 300000)
	collect := func(bit uint8, db *gorm.DB, query string, args ...any) error {
		var norms []string
		if err := db.Raw(query, args...).Scan(&norms).Error; err != nil {
			return err
		}
		for _, n := range norms {
			if runeLen(n) >= bgmGatedMinLen {
				set[n] |= bit
			}
		}
		return nil
	}
	if err := collect(corpusEG, im.eg,
		`SELECT DISTINCT lower(normalize(gamename, NFKC)) FROM games WHERE gamename IS NOT NULL AND gamename <> ''`,
	); err != nil {
		return nil, fmt.Errorf("eg corpus: %w", err)
	}
	if err := collect(corpusDLsite, dlsiteDB,
		`SELECT DISTINCT lower(normalize(work_name, NFKC)) FROM works
			WHERE status = 'fetched' AND work_name IS NOT NULL AND work_type_string = ANY(`+sqlDLsiteGameTypes()+`)`,
	); err != nil {
		return nil, fmt.Errorf("dlsite corpus: %w", err)
	}
	if err := collect(corpusVNDB, im.catalog,
		`SELECT DISTINCT lower(normalize(title, NFKC)) FROM src_vndb.releases_titles WHERE lang = 'ja' AND title IS NOT NULL
			UNION SELECT DISTINCT lower(normalize(latin, NFKC)) FROM src_vndb.releases_titles WHERE lang = 'ja' AND latin IS NOT NULL AND latin <> ''`,
	); err != nil {
		return nil, fmt.Errorf("vndb corpus: %w", err)
	}
	return set, nil
}

func sqlDLsiteGameTypes() string {
	out := "array["
	for i, t := range dlsiteGameTypes {
		if i > 0 {
			out += ","
		}
		out += "'" + t + "'"
	}
	return out + "]"
}

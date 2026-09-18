package intromt

import (
	"context"
	"fmt"

	"api/internal/jobs/workpop"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type Population string

const (
	PopulationBodyless Population = "bodyless"
	PopulationClaimed  Population = "claimed"

	PopulationPublished Population = "published"
)

type SourceLang string

const (
	SourceJa SourceLang = "ja"

	SourceEn SourceLang = "en"
)

type registry struct {
	galgameMedium int16
	dlsiteSource  int16
}

func resolveRegistry(ctx context.Context, db *gorm.DB) (registry, error) {
	var r registry
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&r.galgameMedium).Error; err != nil {
		return r, fmt.Errorf("resolve galgame medium: %w", err)
	}
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = 'dlsite'`).Scan(&r.dlsiteSource).Error; err != nil {
		return r, fmt.Errorf("resolve dlsite source: %w", err)
	}
	if r.galgameMedium == 0 || r.dlsiteSource == 0 {
		return r, fmt.Errorf("registry not seeded (galgame medium=%d, dlsite source=%d)", r.galgameMedium, r.dlsiteSource)
	}
	return r, nil
}

type candidate struct {
	WorkID     int64    `gorm:"column:work_id"`
	JaSourceID int16    `gorm:"column:ja_source_id"`
	JaText     string   `gorm:"column:ja_text"`
	MZhID      *int64   `gorm:"column:mzh_id"`
	MZhSrcHash *string  `gorm:"column:mzh_src_hash"`
	MZhStrays  bool     `gorm:"column:mzh_strays"`
	PopScore   int64    `gorm:"column:pop_score"`
	Gloss      Glossary `gorm:"-"`
}

func loadCandidates(ctx context.Context, db *gorm.DB, reg registry, pop Population, src SourceLang, top, limit int, workIDs []int64) ([]candidate, error) {
	if src == "" {
		src = SourceJa
	}
	if src != SourceJa && src != SourceEn {
		return nil, fmt.Errorf("unknown source language %q (want %q or %q)", src, SourceJa, SourceEn)
	}
	lastResortGate := ""
	if src == SourceEn {
		lastResortGate = `
		  AND NOT EXISTS (SELECT 1 FROM catalog_work_intro j WHERE j.work_id = b.id AND j.lang = 'ja')`
	}
	// A named list is the whole request, so the popularity ceiling stops
	// applying: --work-ids with the default --top 5000 would take the 5,000
	// most popular of the list and report a clean run over the rest.
	unlimited := top <= 0 || len(workIDs) > 0
	sitePredicate, err := sitePredicateFor(pop)
	if err != nil {
		return nil, err
	}
	idGate := ``
	args := []any{reg.galgameMedium, string(src),
		reg.dlsiteSource, model.PopularityMetricDownloads,
		reg.dlsiteSource, model.PopularityMetricWishlist}
	if len(workIDs) > 0 {
		idGate = `
		  AND b.id IN (?)`
		args = append(args, workIDs)
	}
	limitClause := ` LIMIT ?`
	if unlimited {
		limitClause = ``
	} else {
		args = append(args, top)
	}
	// Comparing against the lowest-source machine row while the write landed on the
	// chosen source retranslated ~700 works every night and left readers on the
	// older row (2026-09-18).
	q := db.WithContext(ctx).Raw(`
		WITH pool AS (
			SELECT id FROM catalog_work
			WHERE medium_id = ? AND `+sitePredicate+` AND deleted_at IS NULL
			  AND (olang = '' OR olang = 'ja')
		),
		has_zh_source AS (
			SELECT DISTINCT work_id FROM catalog_work_intro
			WHERE lang IN ('zh-Hans','zh-Hant') AND provenance = 0
		),
		ja AS (
			SELECT DISTINCT ON (work_id) work_id, source_id AS ja_source_id, intro AS ja_text
			FROM catalog_work_intro WHERE lang = ?
			ORDER BY work_id, source_id
		),
		mzh AS (
			SELECT work_id, source_id, id AS mzh_id, src_hash AS mzh_src_hash
			FROM catalog_work_intro WHERE lang = 'zh-Hans' AND provenance = 1
		),
		pop AS (
			SELECT work_id,
				max(value) FILTER (WHERE source_id = ? AND metric = ?) AS dl,
				max(value) FILTER (WHERE source_id = ? AND metric = ?) AS wl
			FROM catalog_work_popularity GROUP BY work_id
		)
		SELECT b.id AS work_id, ja.ja_source_id, ja.ja_text,
			mzh.mzh_id, mzh.mzh_src_hash,
			EXISTS (
				SELECT 1 FROM catalog_work_intro stray
				WHERE stray.work_id = b.id
				  AND stray.lang = 'zh-Hans'
				  AND stray.provenance = 1
				  AND stray.source_id <> ja.ja_source_id
			) AS mzh_strays,
			COALESCE(pop.dl, pop.wl, 0) AS pop_score
		FROM pool b
		JOIN ja ON ja.work_id = b.id
		LEFT JOIN has_zh_source hs ON hs.work_id = b.id
		LEFT JOIN mzh ON mzh.work_id = b.id AND mzh.source_id = ja.ja_source_id
		LEFT JOIN pop ON pop.work_id = b.id
		WHERE hs.work_id IS NULL`+lastResortGate+idGate+`
		ORDER BY COALESCE(pop.dl, pop.wl, 0) DESC, b.id ASC`+limitClause,
		args...)
	var out []candidate
	if err := q.Scan(&out).Error; err != nil {
		return nil, err
	}
	for i := range out {
		out[i].JaText = sanitizeSource(out[i].JaText)
	}
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out, nil
}

// sitePredicateFor renders a population as a catalog_work predicate, over
// unqualified columns (this job's candidate query has no table alias).
//
// The definitions live in workpop so the four lanes that need them cannot
// drift from each other or from model.ClaimStateKey. This job's own vocabulary
// stays narrower on purpose: unlike the enrichment lanes it has no "all", and
// an empty population is an error rather than "everything" — a translation run
// is expensive, and defaulting it to the whole table is not a safe default.
//
// "claimed" is a PROPERTY — has a site — and never the literal site value.
// Wave 161 renamed the only value that has ever existed (galgame_wiki →
// kungal), and every lane that had pinned the literal went silently empty for
// three days while reporting a clean zero-candidate run.
func sitePredicateFor(pop Population) (string, error) {
	switch pop {
	case PopulationBodyless, PopulationClaimed, PopulationPublished:
		return workpop.Predicate(workpop.Population(pop), "")
	default:
		return "", fmt.Errorf("unknown population %q (want %q, %q or %q)",
			pop, PopulationBodyless, PopulationClaimed, PopulationPublished)
	}
}

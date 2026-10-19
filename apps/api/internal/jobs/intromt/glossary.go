package intromt

import (
	"context"
	"strings"

	"api/internal/platform/catalog/editspec"

	"gorm.io/gorm"
)

type GlossaryEntry struct {
	Src string
	Zh  string
}

type Glossary []GlossaryEntry

const (
	maxGlossaryEntries = 20
	maxWorksPerEntity  = 12
	glossaryChunk      = 1000
)

func (g Glossary) Canonical() string {
	if len(g) == 0 {
		return ""
	}
	lines := make([]string, 0, len(g))
	for _, e := range g {
		lines = append(lines, e.Src+"\t"+e.Zh)
	}
	return strings.Join(lines, "\n")
}

type glossaryBuilder struct {
	seen map[string]struct{}
	out  Glossary
}

func (b *glossaryBuilder) add(src, zh string) {
	if len(b.out) >= maxGlossaryEntries {
		return
	}
	src, zh = strings.TrimSpace(src), strings.TrimSpace(zh)
	if src == "" || zh == "" || src == zh {
		return
	}
	if b.seen == nil {
		b.seen = make(map[string]struct{}, maxGlossaryEntries)
	}
	if _, dup := b.seen[src]; dup {
		return
	}
	b.seen[src] = struct{}{}
	b.out = append(b.out, GlossaryEntry{Src: src, Zh: zh})
}

type glossRow struct {
	OwnerID int64  `gorm:"column:owner_id"`
	Src     string `gorm:"column:src"`
	Zh      string `gorm:"column:zh"`
}

var (
	workOwnTitleQuery = `
		WITH t AS (
			SELECT work_id, lang, title, kind, id FROM catalog_work_title wt
			WHERE work_id IN (?) AND kind IN (0,1)
			  AND (lang = 'ja' OR lang IN ('zh-Hans','zh','zh-Hant'))
			  AND ` + editspec.NotSuppressedWorkTitleSQL("wt") + `
		),
		jat AS (
			SELECT DISTINCT ON (work_id) work_id, title FROM t
			WHERE lang = 'ja' ORDER BY work_id, kind, id
		),
		zht AS (
			SELECT DISTINCT ON (work_id) work_id, title FROM t
			WHERE lang <> 'ja' ORDER BY work_id, (lang <> 'zh-Hans'), kind, id
		)
		SELECT jat.work_id AS owner_id, jat.title AS src, zht.title AS zh
		FROM jat JOIN zht ON zht.work_id = jat.work_id
		ORDER BY jat.work_id`

	workRosterQuery = `
		WITH ros AS (
			SELECT wc.work_id, wc.character_id, c.display_name
			FROM catalog_work_character wc
			JOIN catalog_character c ON c.id = wc.character_id AND c.deleted_at IS NULL
			WHERE wc.work_id IN (?)
			  AND ` + editspec.NotSuppressedRosterSQL("wc") + `
		),
		al AS (
			SELECT DISTINCT ON (a.character_id) a.character_id, a.name
			FROM catalog_character_alias a
			WHERE a.character_id IN (SELECT character_id FROM ros)
			  AND a.lang IN ('zh-Hans','zh','zh-Hant') AND a.kind IN (0,1)
			  AND ` + editspec.NotSuppressedCharacterAliasSQL("a") + `
			ORDER BY a.character_id, (NOT a.is_primary_for_locale), (a.lang <> 'zh-Hans'), a.id
		)
		SELECT ros.work_id AS owner_id, ros.display_name AS src, al.name AS zh
		FROM ros JOIN al ON al.character_id = ros.character_id
		ORDER BY ros.work_id, ros.character_id`

	// en-lane only. en source texts carry romaji, which the kanji roster keys never
	// match — work 2775 shipped with "Azabu Masumi" untranslated because its only
	// roster pair was the identity 麻布 真澄→麻布 真澄. The romaji lives in ASCII
	// kind 0/1 aliases (both `latin` columns are empty in prod). Never wire this for
	// SourceJa: it would drift every ja glossary hash and re-trigger the whole lane.
	workRosterLatinQuery = `
		WITH ros AS (
			SELECT wc.work_id, wc.character_id
			FROM catalog_work_character wc
			JOIN catalog_character c ON c.id = wc.character_id AND c.deleted_at IS NULL
			WHERE wc.work_id IN (?)
			  AND ` + editspec.NotSuppressedRosterSQL("wc") + `
		),
		lat AS (
			SELECT DISTINCT ON (a.character_id) a.character_id, a.name
			FROM catalog_character_alias a
			WHERE a.character_id IN (SELECT character_id FROM ros)
			  AND a.name ~ '^[\x20-\x7e]+$' AND a.name ~ '[A-Za-z]' AND a.kind IN (0,1)
			  AND ` + editspec.NotSuppressedCharacterAliasSQL("a") + `
			ORDER BY a.character_id, (a.lang <> 'ja'), a.id
		),
		zhn AS (
			SELECT DISTINCT ON (a.character_id) a.character_id, a.name
			FROM catalog_character_alias a
			WHERE a.character_id IN (SELECT character_id FROM ros)
			  AND a.lang IN ('zh-Hans','zh','zh-Hant') AND a.kind IN (0,1)
			  AND ` + editspec.NotSuppressedCharacterAliasSQL("a") + `
			ORDER BY a.character_id, (NOT a.is_primary_for_locale), (a.lang <> 'zh-Hans'), a.id
		)
		SELECT ros.work_id AS owner_id, lat.name AS src, zhn.name AS zh
		FROM ros
		JOIN lat ON lat.character_id = ros.character_id
		JOIN zhn ON zhn.character_id = ros.character_id
		ORDER BY ros.work_id, ros.character_id`

	workLabelQuery = `
		WITH lab AS (
			SELECT DISTINCT wl.work_id, wl.label_id, l.display_name
			FROM catalog_work_label wl
			JOIN catalog_label l ON l.id = wl.label_id AND l.deleted_at IS NULL
			WHERE wl.work_id IN (?)
		),
		al AS (
			SELECT DISTINCT ON (a.label_id) a.label_id, a.name
			FROM catalog_label_alias a
			WHERE a.label_id IN (SELECT label_id FROM lab)
			  AND a.lang IN ('zh-Hans','zh','zh-Hant') AND a.kind IN (0,1)
			  AND ` + editspec.NotSuppressedLabelAliasSQL("a") + `
			ORDER BY a.label_id, (NOT a.is_primary_for_locale), (a.lang <> 'zh-Hans'), a.id
		)
		SELECT lab.work_id AS owner_id, lab.display_name AS src, al.name AS zh
		FROM lab JOIN al ON al.label_id = lab.label_id
		ORDER BY lab.work_id, lab.label_id`
)

func attachGlossaries(ctx context.Context, db *gorm.DB, cands []candidate, src SourceLang) error {
	ids := make([]int64, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.WorkID)
	}
	gs, err := loadGlossaries(ctx, db, ids, src)
	if err != nil {
		return err
	}
	for i := range cands {
		cands[i].Gloss = gs[cands[i].WorkID]
	}
	return nil
}

func loadGlossaries(ctx context.Context, db *gorm.DB, workIDs []int64, src SourceLang) (map[int64]Glossary, error) {
	queries := []string{workOwnTitleQuery, workRosterQuery, workLabelQuery}
	if src == SourceEn {
		queries = []string{workOwnTitleQuery, workRosterLatinQuery, workRosterQuery, workLabelQuery}
	}
	builders := make(map[int64]*glossaryBuilder, len(workIDs))
	for _, id := range workIDs {
		builders[id] = &glossaryBuilder{}
	}
	for _, q := range queries {
		if err := collectGlossary(ctx, db, q, workIDs, builders); err != nil {
			return nil, err
		}
	}
	out := make(map[int64]Glossary, len(builders))
	for id, b := range builders {
		if len(b.out) > 0 {
			out[id] = b.out
		}
	}
	return out, nil
}

func collectGlossary(ctx context.Context, db *gorm.DB, query string, ids []int64, builders map[int64]*glossaryBuilder) error {
	for start := 0; start < len(ids); start += glossaryChunk {
		end := min(start+glossaryChunk, len(ids))
		var rows []glossRow
		if err := db.WithContext(ctx).Raw(query, ids[start:end]).Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if b := builders[row.OwnerID]; b != nil {
				b.add(row.Src, row.Zh)
			}
		}
	}
	return nil
}

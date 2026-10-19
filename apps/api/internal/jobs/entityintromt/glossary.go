package entityintromt

import (
	"context"
	"strconv"
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
	characterOwnQuery = `
		SELECT a.character_id AS owner_id, c.display_name AS src, a.name AS zh
		FROM catalog_character_alias a
		JOIN catalog_character c ON c.id = a.character_id
		WHERE a.character_id IN (?)
		  AND a.lang IN ('zh-Hans','zh','zh-Hant') AND a.kind IN (0,1)
		  AND ` + editspec.NotSuppressedCharacterAliasSQL("a") + `
		ORDER BY a.character_id, (NOT a.is_primary_for_locale), (a.lang <> 'zh-Hans'), a.id`

	personOwnQuery = `
		SELECT p.id AS owner_id, cn.name AS src, a.name AS zh
		FROM catalog_person p
		JOIN catalog_credit_name cn ON cn.id = p.primary_credit_name_id
		JOIN catalog_name_alias a ON a.credit_name_id = cn.id
		  AND a.lang IN ('zh-Hans','zh','zh-Hant') AND a.kind IN (0,1)
		WHERE p.id IN (?)
		ORDER BY p.id, (NOT a.is_primary_for_locale), (a.lang <> 'zh-Hans'), a.id`

	labelOwnQuery = `
		SELECT a.label_id AS owner_id, l.display_name AS src, a.name AS zh
		FROM catalog_label_alias a
		JOIN catalog_label l ON l.id = a.label_id
		WHERE a.label_id IN (?)
		  AND a.lang IN ('zh-Hans','zh','zh-Hant') AND a.kind IN (0,1)
		  AND ` + editspec.NotSuppressedLabelAliasSQL("a") + `
		ORDER BY a.label_id, (NOT a.is_primary_for_locale), (a.lang <> 'zh-Hans'), a.id`
)

var workTitlePairSQL = `,
		t AS (
			SELECT work_id, lang, title, kind, id FROM catalog_work_title wt
			WHERE work_id IN (SELECT work_id FROM scope) AND kind IN (0,1)
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
		),
		pairs AS (
			SELECT s.owner_id, jat.title AS src, zht.title AS zh,
				row_number() OVER (PARTITION BY s.owner_id ORDER BY s.work_id) AS rn
			FROM scope s
			JOIN jat ON jat.work_id = s.work_id
			JOIN zht ON zht.work_id = s.work_id
		)
		SELECT owner_id, src, zh FROM pairs WHERE rn <= ` + strconv.Itoa(maxWorksPerEntity) + `
		ORDER BY owner_id, rn`

var (
	characterWorksQuery = `
		WITH scope AS (
			SELECT DISTINCT wc.character_id AS owner_id, wc.work_id
			FROM catalog_work_character wc
			JOIN catalog_work w ON w.id = wc.work_id AND w.deleted_at IS NULL
			WHERE wc.character_id IN (?) AND ` + editspec.NotSuppressedRosterSQL("wc") + `
		)` + workTitlePairSQL

	personWorksQuery = `
		WITH scope AS (
			SELECT DISTINCT cn.person_id AS owner_id, cr.work_id
			FROM catalog_credit_name cn
			JOIN catalog_credit cr ON cr.credit_name_id = cn.id
			JOIN catalog_work w ON w.id = cr.work_id AND w.deleted_at IS NULL
			WHERE cn.person_id IN (?) AND ` + editspec.NotSuppressedCreditSQL("cr") + `
		)` + workTitlePairSQL

	labelWorksQuery = `
		WITH scope AS (
			SELECT DISTINCT wl.label_id AS owner_id, wl.work_id
			FROM catalog_work_label wl
			JOIN catalog_work w ON w.id = wl.work_id AND w.deleted_at IS NULL
			WHERE wl.label_id IN (?)
		)` + workTitlePairSQL
)

var laneGlossaryQueries = map[string][]string{
	LaneCharacter: {characterOwnQuery, characterWorksQuery},
	LanePerson:    {personOwnQuery, personWorksQuery},
	LaneLabel:     {labelOwnQuery, labelWorksQuery},
}

func attachGlossaries(ctx context.Context, db *gorm.DB, lane laneDef, cands []candidate) error {
	ids := make([]int64, 0, len(cands))
	for _, c := range cands {
		ids = append(ids, c.EntityID)
	}
	gs, err := loadGlossaries(ctx, db, lane, ids)
	if err != nil {
		return err
	}
	for i := range cands {
		cands[i].Gloss = gs[cands[i].EntityID]
	}
	return nil
}

func loadGlossaries(ctx context.Context, db *gorm.DB, lane laneDef, ids []int64) (map[int64]Glossary, error) {
	builders := make(map[int64]*glossaryBuilder, len(ids))
	for _, id := range ids {
		builders[id] = &glossaryBuilder{}
	}
	for _, q := range laneGlossaryQueries[lane.key] {
		if err := collectGlossary(ctx, db, q, ids, builders); err != nil {
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

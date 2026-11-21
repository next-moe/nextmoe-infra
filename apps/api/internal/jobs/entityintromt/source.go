package entityintromt

import (
	"context"
	"strings"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/introguard"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type SourceLang string

const (
	SourceJa SourceLang = "ja"
	SourceEn SourceLang = "en"
)

type candidate struct {
	EntityID   int64    `gorm:"column:entity_id"`
	SrcLang    string   `gorm:"column:src_lang"`
	SourceID   int16    `gorm:"column:source_id"`
	Text       string   `gorm:"column:src_text"`
	MZhID      *int64   `gorm:"column:mzh_id"`
	MZhSrcHash *string  `gorm:"column:mzh_src_hash"`
	MZhText    *string  `gorm:"column:mzh_text"`
	MZhStrays  bool     `gorm:"column:mzh_strays"`
	HasMachine bool     `gorm:"column:has_machine"`
	Gloss      Glossary `gorm:"-"`
	// NoSource marks an entity whose every ja and en intro is untranslatable
	// (introguard.Untranslatable): its machine rows are cleared, not refreshed.
	NoSource bool `gorm:"-"`
}

func loadCandidates(ctx context.Context, db *gorm.DB, lane laneDef, limit, offset int, entityIDs []int64) ([]candidate, error) {
	t, id := lane.introTable, lane.idCol
	idGate := ``
	args := []any{model.SourceDerived, model.SourceDerived}
	if len(entityIDs) > 0 {
		idGate = `
		  AND src.entity_id IN (?)`
		args = append(args, entityIDs)
	}
	// Every ja and en row comes back, best first: which one is the source is
	// decided in Go, past rows that are only links or punctuation. Comparing
	// against the lowest-source machine row while the write landed on the
	// chosen source retranslated the same characters every night and left
	// readers on the older row (2026-09-18), so mzh joins per row.
	q := `
		WITH src AS (
			SELECT ` + id + ` AS entity_id, lang, source_id, intro,
				row_number() OVER (PARTITION BY ` + id + ` ORDER BY (lang <> 'ja'), ` + editspec.IntroFoldOrderSQL("source_id", "provenance") + `) AS rk
			FROM ` + t + `
			WHERE lang IN ('ja','en') AND provenance = 0 AND ` + editspec.NotSuppressedIntroTableSQL(t) + `
		),
		has_zh_source AS (
			SELECT DISTINCT ` + id + ` AS entity_id FROM ` + t + `
			WHERE lang IN ('zh-Hans','zh-Hant') AND provenance = 0
		),
		mzh AS (
			SELECT ` + id + ` AS entity_id, source_id, id AS mzh_id, src_hash AS mzh_src_hash, intro AS mzh_text
			FROM ` + t + ` WHERE lang = 'zh-Hans' AND provenance = 1
		)
		SELECT src.entity_id, src.lang AS src_lang, src.source_id, src.intro AS src_text,
			mzh.mzh_id, mzh.mzh_src_hash, mzh.mzh_text,
			EXISTS (
				SELECT 1 FROM ` + t + ` stray
				WHERE stray.` + id + ` = src.entity_id
				  AND stray.lang = 'zh-Hans'
				  AND stray.provenance = 1
				  AND stray.source_id <> src.source_id
				  AND stray.source_id <> ?
			) AS mzh_strays,
			EXISTS (
				SELECT 1 FROM ` + t + ` m
				WHERE m.` + id + ` = src.entity_id
				  AND m.lang = 'zh-Hans'
				  AND m.provenance = 1
				  AND m.source_id <> ?
			) AS has_machine
		FROM src
		JOIN ` + lane.entityTable + ` e ON e.id = src.entity_id AND e.deleted_at IS NULL
		LEFT JOIN has_zh_source hz ON hz.entity_id = src.entity_id
		LEFT JOIN mzh ON mzh.entity_id = src.entity_id AND mzh.source_id = src.source_id
		WHERE hz.entity_id IS NULL` + idGate + `
		ORDER BY src.entity_id ASC, src.rk ASC`
	var rows []candidate
	if err := db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	var out []candidate
	for i := 0; i < len(rows); {
		j := i
		for j < len(rows) && rows[j].EntityID == rows[i].EntityID {
			j++
		}
		if c, ok := pickSource(rows[i:j]); ok {
			out = append(out, c)
		}
		i = j
	}
	if offset > 0 {
		if offset >= len(out) {
			return nil, nil
		}
		out = out[offset:]
	}
	if limit > 0 && limit < len(out) {
		out = out[:limit]
	}
	return out, nil
}

func pickSource(rows []candidate) (candidate, bool) {
	for _, r := range rows {
		if strings.TrimSpace(r.Text) == "" || introguard.Untranslatable(r.Text) {
			continue
		}
		return r, true
	}
	c := rows[0]
	c.NoSource = true
	return c, c.HasMachine || strings.TrimSpace(c.Text) != ""
}

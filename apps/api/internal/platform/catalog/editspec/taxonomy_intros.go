package editspec

import (
	"context"
	"fmt"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

type introTable struct {
	ownerCol string
	// Only catalog_label_intro carries the 0/1 provenance axis. catalog_tag_intro
	// and catalog_series_intro have no such column, so filtering on it there is a
	// SQL error rather than a stricter query.
	hasProvenance bool
	empty         func() any
	newRow        func(ownerID int64, lang, intro string) any
	read          func(ctx context.Context, db *gorm.DB, ownerID int64) (map[string]string, error)
}

var introTableLabel = introTable{
	ownerCol:      "label_id",
	hasProvenance: true,
	empty:         func() any { return &catmodel.CatalogLabelIntro{} },
	newRow: func(id int64, lang, intro string) any {
		return &catmodel.CatalogLabelIntro{
			LabelID: id, Lang: lang, Intro: intro,
			SourceID: curatedSourceID, Provenance: catmodel.IntroProvenanceSource,
		}
	},
	read: func(ctx context.Context, db *gorm.DB, ownerID int64) (map[string]string, error) {
		var rows []catmodel.CatalogLabelIntro
		if err := curatedIntroScope(ctx, db, "label_id", ownerID).
			Where("provenance = ?", catmodel.IntroProvenanceSource).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		out := make(map[string]string, len(rows))
		for _, r := range rows {
			out[r.Lang] = r.Intro
		}
		return out, nil
	},
}

var introTableTag = introTable{
	ownerCol: "tag_id",
	empty:    func() any { return &catmodel.CatalogTagIntro{} },
	newRow: func(id int64, lang, intro string) any {
		return &catmodel.CatalogTagIntro{TagID: id, Lang: lang, Intro: intro, SourceID: curatedSourceID}
	},
	read: func(ctx context.Context, db *gorm.DB, ownerID int64) (map[string]string, error) {
		var rows []catmodel.CatalogTagIntro
		if err := curatedIntroScope(ctx, db, "tag_id", ownerID).Find(&rows).Error; err != nil {
			return nil, err
		}
		out := make(map[string]string, len(rows))
		for _, r := range rows {
			out[r.Lang] = r.Intro
		}
		return out, nil
	},
}

var introTableSeries = introTable{
	ownerCol: "series_id",
	empty:    func() any { return &catmodel.CatalogSeriesIntro{} },
	newRow: func(id int64, lang, intro string) any {
		return &catmodel.CatalogSeriesIntro{SeriesID: id, Lang: lang, Intro: intro, SourceID: curatedSourceID}
	},
	read: func(ctx context.Context, db *gorm.DB, ownerID int64) (map[string]string, error) {
		var rows []catmodel.CatalogSeriesIntro
		if err := curatedIntroScope(ctx, db, "series_id", ownerID).Find(&rows).Error; err != nil {
			return nil, err
		}
		out := make(map[string]string, len(rows))
		for _, r := range rows {
			out[r.Lang] = r.Intro
		}
		return out, nil
	},
}

func curatedIntroScope(ctx context.Context, db *gorm.DB, ownerCol string, ownerID int64) *gorm.DB {
	return db.WithContext(ctx).Where(ownerCol+" = ? AND source_id = ?", ownerID, curatedSourceID)
}

func applyEntityIntros(t introTable) editing.ApplyFunc {
	return func(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
		intros, err := parseIntros(value)
		if err != nil {
			return fmt.Errorf("editspec: intros: %w", err)
		}
		del := curatedIntroScope(ctx, tx, t.ownerCol, entityID)
		if t.hasProvenance {
			prior, err := t.read(ctx, tx, entityID)
			if err != nil {
				return err
			}
			if sourceLangChanged(prior, intros) {
				if err := curatedIntroScope(ctx, tx, t.ownerCol, entityID).
					Where("provenance = ?", catmodel.IntroProvenanceMachine).
					Delete(t.empty()).Error; err != nil {
					return err
				}
			}
			langs := make([]string, 0, len(intros))
			for _, in := range intros {
				langs = append(langs, in.Lang)
			}
			if len(langs) == 0 {
				del = del.Where("provenance = ?", catmodel.IntroProvenanceSource)
			} else {
				del = del.Where("provenance = ? OR lang IN ?", catmodel.IntroProvenanceSource, langs)
			}
		}
		if err := del.Delete(t.empty()).Error; err != nil {
			return err
		}
		for _, in := range intros {
			if err := tx.WithContext(ctx).Create(t.newRow(entityID, in.Lang, in.Intro)).Error; err != nil {
				return err
			}
		}
		return nil
	}
}

func loadEntityIntros(ctx context.Context, db *gorm.DB, t introTable, entityID int64) ([]any, error) {
	byLang, err := t.read(ctx, db, entityID)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(byLang))
	for _, lang := range introLangs {
		if intro, ok := byLang[lang]; ok {
			out = append(out, map[string]any{"lang": lang, "intro": intro})
		}
	}
	return out, nil
}

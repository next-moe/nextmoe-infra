package editspec

import (
	"context"
	"fmt"
	"strings"

	"api/internal/platform/catalog/introguard"
	catmodel "api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

var introLangs = []string{"en", "ja", "zh-Hans", "zh-Hant"}

type workIntro struct {
	Lang  string
	Intro string
}

func parseIntros(v any) ([]workIntro, error) {
	arr, err := asArray(v, "intro objects")
	if err != nil {
		return nil, err
	}
	out := make([]workIntro, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	for i, el := range arr {
		obj, err := asObject(el, i, "lang", "intro")
		if err != nil {
			return nil, err
		}
		lang, err := objString(obj, "lang", i, true, 32)
		if err != nil {
			return nil, err
		}
		allowed := false
		for _, l := range introLangs {
			if l == lang {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("element %d: lang must be one of %s", i, strings.Join(introLangs, ", "))
		}
		if _, dup := seen[lang]; dup {
			return nil, fmt.Errorf("element %d: duplicate lang %q", i, lang)
		}
		seen[lang] = struct{}{}
		intro, err := objString(obj, "intro", i, true, maxIntroRunes)
		if err != nil {
			return nil, err
		}
		out = append(out, workIntro{Lang: lang, Intro: intro})
	}
	return out, nil
}

func validateIntros(v any) error {
	intros, err := parseIntros(v)
	if err != nil {
		return err
	}
	for i, in := range intros {
		if introguard.Untranslatable(in.Intro) {
			return fmt.Errorf("element %d: the %s intro has no description in it, only links, punctuation or text copied from a site's page; remove it or write the intro", i, in.Lang)
		}
	}
	return nil
}

// sourceLangChanged reports whether an intro edit changes the ja or en text the
// machine translation was made from. The lanes key their row on the source, so
// without this the curated zh-Hans machine row kept translating the replaced
// text: work 279247's forum page showed the model's answer to a VNDB voting
// prompt for a day after the Japanese intro had been written (2026-11-21).
func sourceLangChanged(before map[string]string, after []workIntro) bool {
	next := make(map[string]string, len(after))
	for _, in := range after {
		next[in.Lang] = in.Intro
	}
	for _, lang := range []string{"ja", "en"} {
		if before[lang] != next[lang] {
			return true
		}
	}
	return false
}

func applyIntros(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	intros, err := parseIntros(value)
	if err != nil {
		return fmt.Errorf("editspec: intros: %w", err)
	}
	if err := assertWorkExists(ctx, tx, entityID); err != nil {
		return err
	}
	var before []catmodel.CatalogWorkIntro
	if err := tx.WithContext(ctx).
		Where("work_id = ? AND source_id = ? AND provenance = ?", entityID, curatedSourceID, catmodel.IntroProvenanceSource).
		Find(&before).Error; err != nil {
		return err
	}
	prior := make(map[string]string, len(before))
	for _, r := range before {
		prior[r.Lang] = r.Intro
	}
	if sourceLangChanged(prior, intros) {
		if err := tx.WithContext(ctx).
			Where("work_id = ? AND source_id = ? AND provenance = ?", entityID, curatedSourceID, catmodel.IntroProvenanceMachine).
			Delete(&catmodel.CatalogWorkIntro{}).Error; err != nil {
			return err
		}
	}
	langs := make([]string, 0, len(intros))
	for _, in := range intros {
		langs = append(langs, in.Lang)
	}
	del := tx.WithContext(ctx).
		Where("work_id = ? AND source_id = ?", entityID, curatedSourceID).
		Where("provenance = ? OR lang IN ?", catmodel.IntroProvenanceSource, langs)
	if len(langs) == 0 {
		del = tx.WithContext(ctx).
			Where("work_id = ? AND source_id = ? AND provenance = ?",
				entityID, curatedSourceID, catmodel.IntroProvenanceSource)
	}
	if err := del.Delete(&catmodel.CatalogWorkIntro{}).Error; err != nil {
		return err
	}
	if len(intros) == 0 {
		return nil
	}
	rows := make([]catmodel.CatalogWorkIntro, 0, len(intros))
	for _, in := range intros {
		rows = append(rows, catmodel.CatalogWorkIntro{
			WorkID: entityID, Lang: in.Lang, Intro: in.Intro,
			SourceID: curatedSourceID, Provenance: catmodel.IntroProvenanceSource,
		})
	}
	return tx.WithContext(ctx).Create(&rows).Error
}

func loadIntros(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []catmodel.CatalogWorkIntro
	if err := db.WithContext(ctx).
		Where("work_id = ? AND source_id = ? AND provenance = ?",
			workID, curatedSourceID, catmodel.IntroProvenanceSource).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	byLang := make(map[string]string, len(rows))
	for _, r := range rows {
		byLang[r.Lang] = r.Intro
	}
	out := make([]any, 0, len(rows))
	for _, lang := range introLangs {
		if intro, ok := byLang[lang]; ok {
			out = append(out, map[string]any{"lang": lang, "intro": intro})
		}
	}
	return out, nil
}

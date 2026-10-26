package editspec

import (
	"context"
	"fmt"
	"strings"

	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/textnorm"

	"gorm.io/gorm"
)

const (
	titleKindOfficial     = int64(catmodel.WorkTitleKindOfficial)
	titleKindAlias        = int64(catmodel.WorkTitleKindAlias)
	titleKindAbbreviation = int64(catmodel.WorkTitleKindAbbreviation)

	maxTitleElements = 100
	maxTitleRunes    = 500
)

type workTitle struct {
	Lang  string
	Title string
	Latin string
	Kind  int64
}

func parseTitles(v any) ([]workTitle, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("must be an array of title objects")
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("must contain at least one title")
	}
	if len(arr) > maxTitleElements {
		return nil, fmt.Errorf("must contain at most %d titles", maxTitleElements)
	}
	out := make([]workTitle, 0, len(arr))
	seen := make(map[string]struct{}, len(arr))
	officials := 0
	for i, el := range arr {
		obj, ok := el.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("element %d: must be an object", i)
		}
		for key := range obj {
			switch key {
			case "lang", "title", "kind", "latin":
			default:
				return nil, fmt.Errorf("element %d: unknown key %q", i, key)
			}
		}
		lang := ""
		if raw, present := obj["lang"]; present && raw != nil {
			if lang, ok = raw.(string); !ok {
				return nil, fmt.Errorf("element %d: lang must be a string", i)
			}
		}
		title, ok := obj["title"].(string)
		if !ok {
			return nil, fmt.Errorf("element %d: title must be a string", i)
		}
		if strings.TrimSpace(title) == "" {
			return nil, fmt.Errorf("element %d: title must not be empty", i)
		}
		if len([]rune(title)) > maxTitleRunes {
			return nil, fmt.Errorf("element %d: title must be at most %d characters", i, maxTitleRunes)
		}
		kindF, ok := obj["kind"].(float64)
		if !ok || kindF != float64(int64(kindF)) {
			return nil, fmt.Errorf("element %d: kind must be an integer", i)
		}
		kind := int64(kindF)
		if kind < titleKindOfficial || kind > titleKindAbbreviation {
			return nil, fmt.Errorf("element %d: kind must be 0 (official), 1 (alias) or 2 (abbreviation)", i)
		}
		// An ALIAS may carry the empty language, and only an alias may. An alias
		// is a string people also call the work by — a fan abbreviation, a
		// romanization, a store's spelling — and it belongs to no language in
		// particular; the mirror that filled this table has written alias rows
		// with lang='' from the start (41,373 of them), and the strict rule made
		// this field unable to read back its own table: LoadSnapshot returned
		// lang:"" and the validator then rejected the value it had just produced,
		// so a full-replace edit silently reaped every alias row. Officials keep
		// the strict rule — an official title is the work's name IN a language,
		// and it is the row olang selects the display name from.
		if lang == "" {
			if kind != titleKindAlias {
				return nil, fmt.Errorf("element %d: only an alias (kind 1) may have an empty lang", i)
			}
		} else if _, allowed := olangAllowed[lang]; !allowed {
			return nil, fmt.Errorf("element %d: %q is not an allowed language", i, lang)
		}
		latin := ""
		if raw, present := obj["latin"]; present && raw != nil {
			if latin, ok = raw.(string); !ok {
				return nil, fmt.Errorf("element %d: latin must be a string", i)
			}
			if strings.TrimSpace(latin) == "" {
				latin = ""
			}
			if len([]rune(latin)) > maxTitleRunes {
				return nil, fmt.Errorf("element %d: latin must be at most %d characters", i, maxTitleRunes)
			}
		}
		uniq := fmt.Sprintf("%s\x00%s\x00%d", lang, title, kind)
		if _, dup := seen[uniq]; dup {
			return nil, fmt.Errorf("element %d: duplicate (lang, title, kind)", i)
		}
		seen[uniq] = struct{}{}
		if kind == titleKindOfficial {
			officials++
		}
		out = append(out, workTitle{Lang: lang, Title: title, Latin: latin, Kind: kind})
	}
	if officials == 0 {
		return nil, fmt.Errorf("must contain at least one official (kind 0) title")
	}
	return out, nil
}

func validateTitles(v any) error {
	_, err := parseTitles(v)
	return err
}

func deriveDisplayName(titles []workTitle, olang string) string {
	first := ""
	for _, t := range titles {
		if t.Kind != titleKindOfficial {
			continue
		}
		if t.Lang == olang {
			return t.Title
		}
		if first == "" {
			first = t.Title
		}
	}
	return first
}

func applyTitles(ctx context.Context, tx *gorm.DB, entityID int64, value any) error {
	titles, err := parseTitles(value)
	if err != nil {
		return fmt.Errorf("editspec: titles: %w", err)
	}
	var work catmodel.CatalogWork
	if err := tx.WithContext(ctx).Select("id", "olang").First(&work, entityID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return editing.ErrEntityNotFound
		}
		return err
	}
	if err := tx.WithContext(ctx).
		Where("work_id = ? AND kind IN ?", entityID,
			[]int16{catmodel.WorkTitleKindOfficial, catmodel.WorkTitleKindAlias, catmodel.WorkTitleKindAbbreviation}).
		Delete(&catmodel.CatalogWorkTitle{}).Error; err != nil {
		return err
	}
	rows := make([]catmodel.CatalogWorkTitle, 0, len(titles))
	for _, t := range titles {
		row := catmodel.CatalogWorkTitle{WorkID: entityID, Lang: t.Lang, Title: t.Title, Kind: int16(t.Kind)}
		if t.Latin != "" {
			latin := t.Latin
			row.Latin = &latin
		}
		rows = append(rows, row)
	}
	if err := tx.WithContext(ctx).Create(&rows).Error; err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&catmodel.CatalogWork{}).
		Where("id = ?", entityID).
		Update("display_name", deriveDisplayName(titles, work.OLang)).Error
}

// WorkTitleIdentity is the frozen row identity of catalog.work.titles (doc 21
// D10: registered means never redefined — no hashing, no normalising, no
// reordering, ever). It is derived from the row's content and never from
// catalog_work_title.id, because an importer that deletes and re-inserts the
// same alias hands it a new id, which would expire the suppression exactly when
// it is needed. Kind is decimal and lang carries no colon, so the title is the
// unescaped remainder and needs no quoting.
func WorkTitleIdentity(kind int16, lang, title string) string {
	return fmt.Sprintf("title:%d:%s:%s", kind, lang, textnorm.Clean(title))
}

// WorkTitleIdentitySQL restates WorkTitleIdentity for a catalog_work_title
// alias — the one place the two definitions could drift apart, which is why
// TestWorkTitleIdentitySQLMatchesGo recomputes both over real rows.
func WorkTitleIdentitySQL(alias string) string {
	return fmt.Sprintf(`('title:' || %[1]s.kind || ':' || %[1]s.lang || ':' || %[2]s)`,
		alias, textnorm.CleanSQL(alias+".title"))
}

// NotSuppressedWorkTitleSQL is the read-path predicate, correlated on a
// catalog_work_title alias.
func NotSuppressedWorkTitleSQL(alias string) string {
	return fmt.Sprintf(
		`NOT EXISTS (SELECT 1 FROM edit_suppressed_row esr
			WHERE esr.entity_type = '%s' AND esr.field_key = '%s'
			  AND esr.entity_id = %s.work_id AND esr.identity_key = %s)`,
		TypeWork, FieldWorkTitles, alias, WorkTitleIdentitySQL(alias))
}

func TitleSuppressed(suppressed map[string]struct{}, kind int16, lang, title string) bool {
	if len(suppressed) == 0 {
		return false
	}
	_, ok := suppressed[WorkTitleIdentity(kind, lang, title)]
	return ok
}

func SuppressedWorkTitles(ctx context.Context, db *gorm.DB, workIDs []int64) (map[int64]map[string]struct{}, error) {
	return editing.SuppressedKeysFor(ctx, db, TypeWork, FieldWorkTitles, workIDs)
}

func loadSuppressedTitles(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	return editing.LoadSuppressedValue(ctx, db, TypeWork, workID, FieldWorkTitles)
}

func loadTitles(ctx context.Context, db *gorm.DB, workID int64) ([]any, error) {
	var rows []catmodel.CatalogWorkTitle
	if err := db.WithContext(ctx).
		Where("work_id = ? AND kind <> ?", workID, catmodel.WorkTitleKindSearchHint).
		Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		el := map[string]any{"lang": r.Lang, "title": r.Title, "kind": int64(r.Kind)}
		if r.Latin != nil && *r.Latin != "" {
			el["latin"] = *r.Latin
		}
		out = append(out, el)
	}
	return out, nil
}

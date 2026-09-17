package main

import (
	"context"
	"fmt"
	"os"
	"unicode"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const langZhHans = "zh-Hans"

// noZhCandidate is a live character that carries NO zh-family alias row at all
// — the shared candidate universe of the passthrough and --mt lanes; the pure-
// Han test splits it between them.
const noZhCandidateSQL = `
	SELECT c.id, c.display_name
	FROM catalog_character c
	WHERE c.deleted_at IS NULL
	  AND NOT EXISTS (
	    SELECT 1 FROM catalog_character_alias a
	    WHERE a.character_id = c.id AND a.lang IN ('zh-Hans','zh','zh-Hant'))
	ORDER BY c.id`

type charRow struct {
	ID          int64  `gorm:"column:id"`
	DisplayName string `gorm:"column:display_name"`
}

// pureHan reports whether the name consists only of Han ideographs (plus
// plain / ideographic spaces), i.e. the glyphs themselves already read as
// Chinese. Any kana, romaji, digit, middle dot or long-vowel mark disqualifies
// — those names go to the --mt lane instead. Deliberately stricter than
// bgmzhnames.isChineseName: an identity write asserts the WHOLE string renders
// as Chinese, not merely that some of it does.
func pureHan(s string) bool {
	han := false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			han = true
		case r == ' ' || r == '　':
		default:
			return false
		}
	}
	return han
}

func runPassthrough(ctx context.Context, db *gorm.DB, apply bool, limit, samples int) error {
	var rows []charRow
	if err := db.WithContext(ctx).Raw(noZhCandidateSQL).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load no-zh characters: %w", err)
	}
	cands := make([]charRow, 0, len(rows))
	for _, r := range rows {
		if pureHan(r.DisplayName) {
			cands = append(cands, r)
		}
	}
	pureCount := len(cands)
	residue := len(rows) - pureCount
	if limit > 0 && len(cands) > limit {
		cands = cands[:limit]
	}

	fmt.Printf("\n=== backfill-char-zh-names passthrough (%s) ===\n", mode(apply))
	fmt.Printf("no_zh_characters=%d  pure_han=%d  mt_residue=%d  this_run=%d\n",
		len(rows), pureCount, residue, len(cands))
	for i, c := range cands {
		if i >= samples {
			break
		}
		fmt.Printf("  sample: %d %s\n", c.ID, c.DisplayName)
	}
	if !apply {
		fmt.Println("\n[dry run] nothing written — re-run with --apply")
		return nil
	}

	var inserted, conflict, errs int
	touched := make([]int64, 0, len(cands))
	for _, c := range cands {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		res := db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "character_id"}, {Name: "name"}, {Name: "lang"}},
			DoNothing: true,
		}).Create(&model.CatalogCharacterAlias{
			CharacterID: c.ID,
			Name:        c.DisplayName,
			Lang:        langZhHans,
			Kind:        model.AliasKindSpellingVariant,
			// The candidate query proved the character has no zh row, so the
			// identity row claims the locale primary — that is what makes it
			// reach localized{}. A dict name written first excludes the
			// character from this run entirely (run order, see package doc).
			IsPrimaryForLocale: true,
			Provenance:         model.AliasProvenanceSource,
		}).Error
		if res != nil {
			errs++
			continue
		}
		inserted++
		touched = append(touched, c.ID)
	}
	works, err := hostWorks(ctx, db, touched)
	if err != nil {
		return err
	}
	if err := repository.TouchWorks(ctx, db, works); err != nil {
		return fmt.Errorf("touch host works: %w", err)
	}
	fmt.Printf("\ninserted=%d conflict=%d errors=%d touched_works=%d\n", inserted, conflict, errs, len(works))
	if errs > 0 {
		os.Exit(1)
	}
	return nil
}

const hostWorkChunk = 5000

// hostWorks resolves the distinct live works whose read face renders the
// touched characters — the set whose updated_at must move so the public
// changes feed re-serves them.
func hostWorks(ctx context.Context, db *gorm.DB, characterIDs []int64) ([]int64, error) {
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(characterIDs))
	for start := 0; start < len(characterIDs); start += hostWorkChunk {
		end := min(start+hostWorkChunk, len(characterIDs))
		var ids []int64
		if err := db.WithContext(ctx).Raw(`SELECT DISTINCT wc.work_id
			FROM catalog_work_character wc
			JOIN catalog_work w ON w.id = wc.work_id AND w.deleted_at IS NULL
			WHERE wc.character_id IN ?`, characterIDs[start:end]).Scan(&ids).Error; err != nil {
			return nil, fmt.Errorf("resolve host works: %w", err)
		}
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out, nil
}

func mode(apply bool) string {
	if apply {
		return "APPLY"
	}
	return "DRY"
}

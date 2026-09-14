package service

import (
	"strconv"
	"strings"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WorkDupeMinRunes is the shortest folded norm the dedup lanes compare on;
// shorter strings ("PC版", "同人誌") collide by genre, not identity.
const WorkDupeMinRunes = 4

// WorkTitleFoldSQL folds an already NFKC-lowered SQL text expression for
// duplicate comparison by removing every whitespace rune and then any trailing
// wave-dash run. The 2026-07 bgm import wave minted ~4.9k duplicate works past
// a gate that compared raw title_norm equality; whitespace variants of the same
// title were one of the two holes, so every dup detector and mint guard
// compares folded norms through this one definition.
//
// The trailing wave dash is the other half of the same wound: sources disagree
// about whether a subtitle keeps its closing delimiter, so work 16269
// ("隣人妄想～団地族の昼下がり～") and bgm 222199 (the same title without the
// final ～) were two live works that no lane could see: no shared external id,
// and the norms differed by one rune so the equality gate never fired.
func WorkTitleFoldSQL(expr string) string {
	noSpace := `regexp_replace(` + expr + `, '[[:space:]　]', '', 'g')`
	return `regexp_replace(` + noSpace + `, '[~～〜]+$', '')`
}

func WorkDupeNormEligibleSQL(expr string) string {
	return `(length(` + expr + `) >= 4 OR ` + expr + ` ~ '^[一-鿿]{3}$')`
}

// The 0x4E00..0x9FFF range must equal WorkDupeNormEligibleSQL's [一-鿿] class.
func WorkDupeNormEligible(n string) bool {
	runes := []rune(n)
	if len(runes) >= WorkDupeMinRunes {
		return true
	}
	if len(runes) != 3 {
		return false
	}
	for _, r := range runes {
		if r < 0x4E00 || r > 0x9FFF {
			return false
		}
	}
	return true
}

// WorkDupeCorpusSQL selects every live work's folded identity norms as
// (work_id, n, official): official and alias titles plus display_name, with
// official=false marking alias-sourced norms so the verdict can refuse to
// auto-merge on alias evidence alone. display_name must be in the corpus —
// wiki-claimed works were born with zero title rows, which is the other hole
// the 2026-07 wave slipped through.
func WorkDupeCorpusSQL() string {
	foldTitle := WorkTitleFoldSQL("t.title_norm")
	foldDisplay := WorkTitleFoldSQL("lower(normalize(w.display_name, NFKC))")
	return `SELECT t.work_id, ` + foldTitle + ` AS n, (t.kind = 0) AS official
		FROM catalog_work_title t
		JOIN catalog_work tw ON tw.id = t.work_id AND tw.deleted_at IS NULL
		WHERE t.kind IN (0, 1) AND ` + WorkDupeNormEligibleSQL(foldTitle) + `
		  AND ` + editspec.NotSuppressedWorkTitleSQL("t") + `
		UNION
		SELECT w.id, ` + foldDisplay + `, true AS official
		FROM catalog_work w
		WHERE w.deleted_at IS NULL AND ` + WorkDupeNormEligibleSQL(foldDisplay)
}

type WorkDupeSuspect struct {
	WorkID      int64  `gorm:"column:work_id"`
	DisplayName string `gorm:"column:display_name"`
}

// maxWorkDupeSuspects caps the pre-mint suspect list; the answer is a confirm
// prompt for a human, not a census.
const maxWorkDupeSuspects = 8

func findWorkDupeSuspectsByNames(tx *gorm.DB, mediumID int16, names []string) ([]WorkDupeSuspect, error) {
	vals := make([]string, 0, len(names))
	args := make([]any, 0, len(names)+2)
	for _, n := range names {
		if strings.TrimSpace(n) == "" {
			continue
		}
		vals = append(vals, "(CAST(? AS text))")
		args = append(args, n)
	}
	if len(vals) == 0 {
		return nil, nil
	}
	fold := WorkTitleFoldSQL("lower(normalize(i.raw, NFKC))")
	args = append(args, mediumID)
	var out []WorkDupeSuspect
	err := tx.Raw(`
		WITH input(raw) AS (VALUES `+strings.Join(vals, ", ")+`),
		folded AS (SELECT DISTINCT `+fold+` AS n FROM input i),
		corpus AS (`+WorkDupeCorpusSQL()+`)
		SELECT c.work_id, w.display_name
		FROM folded f
		JOIN corpus c ON c.n = f.n
		JOIN catalog_work w ON w.id = c.work_id AND w.deleted_at IS NULL AND w.medium_id = ?
		WHERE `+WorkDupeNormEligibleSQL("f.n")+`
		GROUP BY c.work_id, w.display_name
		ORDER BY c.work_id
		LIMIT `+strconv.Itoa(maxWorkDupeSuspects), args...).Scan(&out).Error
	return out, err
}

// recordWorkDupeSuspects files a pending match candidate for every live work
// sharing a folded identity norm with the freshly minted work. It never
// blocks the mint — same-name distinct works are real, so the receipt goes
// to the reconciliation queue instead of the submitter. Works of a different
// medium never pair: the bangumi cross-media import deliberately mints an
// adaptation under the same title, and title-only suspects had filed ~1.3k
// cross-medium receipts before the medium predicate was added.
func recordWorkDupeSuspects(tx *gorm.DB, workID int64) error {
	var hits []int64
	err := tx.Raw(`
		WITH corpus AS (`+WorkDupeCorpusSQL()+`)
		SELECT DISTINCT o.work_id
		FROM corpus mine
		JOIN corpus o ON o.n = mine.n AND o.work_id <> mine.work_id
		JOIN catalog_work wm ON wm.id = mine.work_id
		JOIN catalog_work wo ON wo.id = o.work_id AND wo.medium_id = wm.medium_id
		WHERE mine.work_id = ? AND `+WorkDupeNormEligibleSQL("mine.n")+`
		ORDER BY o.work_id`, workID).Scan(&hits).Error
	if err != nil || len(hits) == 0 {
		return err
	}
	rows := make([]model.CatalogMatchCandidate, 0, len(hits))
	for _, hit := range hits {
		a, b := workID, hit
		if b < a {
			a, b = b, a
		}
		rows = append(rows, model.CatalogMatchCandidate{
			EntityType: model.EntityTypeWork, AID: a, BID: b,
			Reason: model.CandidateReasonNameNormEqual,
			Status: model.CandidateStatusPending,
		})
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

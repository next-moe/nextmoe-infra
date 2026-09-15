package llmsuggest

import (
	"strings"

	"api/internal/platform/catalog/service"

	"gorm.io/gorm"
)

// exclusiveNameHolders is the whole gate in one number: the pair itself, and
// nobody else in the catalog.
//
// Three holders is not a near miss, it is the counterexample class. Of the
// needs_manual pairs on 2026-09-15 whose two works lead with the same name, the
// ones a third work also answers to are Memoria, Again, Alive, Real, Faith,
// Flower, Nocturne, Passage, The Inn, Nevermore and Fantasia -- one-word
// English titles that several unrelated visual novels carry. Three of them are
// one upstream row facing two different catalog works (223320 "The Inn" is
// claimed by 24091 and 34827), so at most one of the two pairs can be right and
// a merge picks by row order.
const exclusiveNameHolders = 2

// pairEvidence is what the apply gate knows that the judge did not. The dossier
// carries both works' own fields; this carries a fact about the other 224,000
// works, namely how many of them answer to the name these two share.
type pairEvidence struct {
	Name    string
	Holders int
}

func (e pairEvidence) exclusive() bool { return e.Name != "" && e.Holders == exclusiveNameHolders }

// workPairNameEvidence answers, for each verdict row, whether both works lead
// with the same name and whether that name belongs to them alone.
//
// It compares display_name and not the title list. Matching anywhere in the
// title list is what the seeding rule already did, and it puts a 1999 Japanese
// eroge and a 2022 English one together the moment a localised or alias row
// happens to collide: works 16532 and 27910 (ラブレッスン and Lessons in Love)
// share an exclusive folded norm through a secondary title row and share
// nothing else. The name a record leads with is the one claim it makes about
// its own identity.
//
// The fold and the corpus come from service so this reads identity exactly as
// the detector that proposed the pair and the gate that guards minting do.
func workPairNameEvidence(db *gorm.DB, rows []QueueVerdict) (map[int64]pairEvidence, error) {
	type pairKey struct{ a, b int64 }
	out := make(map[int64]pairEvidence, len(rows))
	ids := map[pairKey][]int64{}
	vals := make([]string, 0, len(rows))
	args := make([]any, 0, len(rows)*2)
	for _, r := range rows {
		out[r.ID] = pairEvidence{}
		k := pairKey{r.AID, r.BID}
		if _, seen := ids[k]; !seen {
			vals = append(vals, "(CAST(? AS bigint), CAST(? AS bigint))")
			args = append(args, r.AID, r.BID)
		}
		ids[k] = append(ids[k], r.ID)
	}
	if len(vals) == 0 {
		return out, nil
	}

	foldA := service.WorkTitleFoldSQL(`lower(normalize(wa.display_name, NFKC))`)
	foldB := service.WorkTitleFoldSQL(`lower(normalize(wb.display_name, NFKC))`)
	var found []struct {
		AID     int64  `gorm:"column:a_id"`
		BID     int64  `gorm:"column:b_id"`
		Name    string `gorm:"column:n"`
		Holders int    `gorm:"column:holders"`
	}
	if err := db.Raw(`
		WITH pair(a_id, b_id) AS (VALUES `+strings.Join(vals, ", ")+`),
		named AS (
			SELECT p.a_id, p.b_id, `+foldA+` AS n
			FROM pair p
			JOIN catalog_work wa ON wa.id = p.a_id AND wa.deleted_at IS NULL
			JOIN catalog_work wb ON wb.id = p.b_id AND wb.deleted_at IS NULL
			WHERE `+foldA+` = `+foldB+` AND `+service.WorkDupeNormEligibleSQL(foldA)+`
		),
		corpus AS (`+service.WorkDupeCorpusSQL()+`)
		SELECT n.a_id, n.b_id, n.n, count(DISTINCT c.work_id) AS holders
		FROM named n JOIN corpus c ON c.n = n.n
		GROUP BY 1, 2, 3`, args...).Scan(&found).Error; err != nil {
		return nil, err
	}
	for _, f := range found {
		for _, id := range ids[pairKey{f.AID, f.BID}] {
			out[id] = pairEvidence{Name: f.Name, Holders: f.Holders}
		}
	}
	return out, nil
}

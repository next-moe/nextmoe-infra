package llmsuggest

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
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

// 450 catalog pairs share an exclusive letters-and-digits display key while
// carrying different exact VNDB ids (mine, puppy, sleepover, lastmemory). Keys
// shorter than five runes are that class; a same verdict still needs the pair
// to be the only two holders of a longer one. Measured 2026-09-18: 279 of the
// 672 same pairs in needs_manual would pass that longer key.
const looseNameMinRunes = 5

const (
	matchedByEGXlinkTwin  = "rule:eg-xlink-twin"
	matchedByEGXlinkMulti = "rule:eg-xlink-multi"
)

// Identity records from these sources can corroborate a pair. A shared
// ErogameScape bundle row (matched_by rule:eg-xlink-multi) is not one of them:
// 999 EG cross-link twins are one game minted twice, but a bundle record names
// two different games on purpose.
var identityRecordSourceIDs = []int16{2, 3, 4, 5, 17}

// pairEvidence is what the apply gate knows that the judge did not. The dossier
// carries both works' own fields; this carries a fact about the other 224,000
// works, namely how many of them answer to the name these two share, and
// whether they share an identity record no third work holds.
type pairEvidence struct {
	Name    string
	Holders int

	LooseName    string
	LooseHolders int

	SharedSourceKey  string
	SharedExternalID string
	SharedHolders    int
}

func (e pairEvidence) exclusive() bool { return e.Name != "" && e.Holders == exclusiveNameHolders }

func (e pairEvidence) exclusiveLoose() bool {
	return e.LooseName != "" && e.LooseHolders == exclusiveNameHolders && utf8.RuneCountInString(e.LooseName) >= looseNameMinRunes
}

func (e pairEvidence) exclusiveShared() bool {
	return e.SharedSourceKey != "" && e.SharedExternalID != "" && e.SharedHolders == exclusiveNameHolders
}

// acceptReason names the corroborator that decides an accept, in preference
// order folded name, shared record, loose name. unsure cannot use the loose
// key: that is the 450-pair VNDB-disagreement class above.
func (e pairEvidence) acceptReason(verdict string) string {
	if e.exclusive() {
		return fmt.Sprintf("sole holders of %q", e.Name)
	}
	if e.exclusiveShared() {
		return fmt.Sprintf("shares %s:%s", e.SharedSourceKey, e.SharedExternalID)
	}
	if verdict == VerdictSame && e.exclusiveLoose() {
		return fmt.Sprintf("sole holders of loose %q", e.LooseName)
	}
	return ""
}

func loadPairEvidence(db *gorm.DB, rows []QueueVerdict) (map[int64]pairEvidence, error) {
	out, err := workPairNameEvidence(db, rows)
	if err != nil {
		return nil, err
	}
	if err := attachLooseNameEvidence(db, rows, out); err != nil {
		return nil, err
	}
	if err := attachSharedRecordEvidence(db, rows, out); err != nil {
		return nil, err
	}
	return out, nil
}

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

func attachLooseNameEvidence(db *gorm.DB, rows []QueueVerdict, out map[int64]pairEvidence) error {
	if len(rows) == 0 {
		return nil
	}
	foldTitle := service.WorkTitleFoldSQL("t.title_norm")
	foldDisplay := service.WorkTitleFoldSQL("lower(normalize(w.display_name, NFKC))")
	var corpus []struct {
		WorkID int64  `gorm:"column:work_id"`
		Raw    string `gorm:"column:raw"`
	}
	// Membership mirrors WorkDupeCorpusSQL (live works, kind 0/1 titles that
	// are not suppressed, eligible folded norms). The key itself is titleKey
	// in Go: PostgreSQL [:alnum:] follows the database locale, and the test
	// database need not match production.
	sql := `SELECT t.work_id, t.title AS raw
		FROM catalog_work_title t
		JOIN catalog_work tw ON tw.id = t.work_id AND tw.deleted_at IS NULL
		WHERE t.kind IN (0, 1) AND ` + service.WorkDupeNormEligibleSQL(foldTitle) + `
		  AND ` + editspec.NotSuppressedWorkTitleSQL("t") + `
		UNION
		SELECT w.id, w.display_name
		FROM catalog_work w
		WHERE w.deleted_at IS NULL AND ` + service.WorkDupeNormEligibleSQL(foldDisplay)
	if err := db.Raw(sql).Scan(&corpus).Error; err != nil {
		return err
	}
	holders := map[string]map[int64]struct{}{}
	for _, row := range corpus {
		k := titleKey(row.Raw)
		if k == "" {
			continue
		}
		if holders[k] == nil {
			holders[k] = map[int64]struct{}{}
		}
		holders[k][row.WorkID] = struct{}{}
	}

	ids := verdictWorkIDs(rows)
	if len(ids) == 0 {
		return nil
	}
	var names []struct {
		ID   int64  `gorm:"column:id"`
		Name string `gorm:"column:display_name"`
	}
	if err := db.Raw(`SELECT id, display_name FROM catalog_work WHERE id IN ? AND deleted_at IS NULL`, ids).Scan(&names).Error; err != nil {
		return err
	}
	display := map[int64]string{}
	for _, n := range names {
		display[n.ID] = titleKey(n.Name)
	}
	for _, r := range rows {
		ka, kb := display[r.AID], display[r.BID]
		if ka == "" || ka != kb {
			continue
		}
		ev := out[r.ID]
		ev.LooseName = ka
		ev.LooseHolders = len(holders[ka])
		out[r.ID] = ev
	}
	return nil
}

type workIdentityRef struct {
	WorkID     int64
	SourceID   int16
	SourceKey  string
	ExternalID string
	LinkKind   int16
	MatchedBy  string
}

type identityRefKey struct {
	SourceID   int16
	ExternalID string
}

func identityBearingRef(linkKind int16, matchedBy string) bool {
	switch linkKind {
	case model.LinkKindExact, model.LinkKindProbable:
		return true
	case model.LinkKindRelated:
		return matchedBy == matchedByEGXlinkTwin
	default:
		return false
	}
}

func pickSharedRecord(a, b []workIdentityRef, holders map[identityRefKey]int) (sourceKey, ext string, n int) {
	type hit struct {
		key       identityRefKey
		sourceKey string
	}
	inA := map[identityRefKey]string{}
	for _, r := range a {
		if !identityBearingRef(r.LinkKind, r.MatchedBy) {
			continue
		}
		inA[identityRefKey{r.SourceID, r.ExternalID}] = r.SourceKey
	}
	var hits []hit
	seen := map[identityRefKey]struct{}{}
	for _, r := range b {
		if !identityBearingRef(r.LinkKind, r.MatchedBy) {
			continue
		}
		k := identityRefKey{r.SourceID, r.ExternalID}
		sk, ok := inA[k]
		if !ok {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		if holders[k] != exclusiveNameHolders {
			continue
		}
		if sk == "" {
			sk = r.SourceKey
		}
		hits = append(hits, hit{key: k, sourceKey: sk})
	}
	if len(hits) == 0 {
		return "", "", 0
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].key.SourceID != hits[j].key.SourceID {
			return hits[i].key.SourceID < hits[j].key.SourceID
		}
		return hits[i].key.ExternalID < hits[j].key.ExternalID
	})
	h := hits[0]
	return h.sourceKey, h.key.ExternalID, exclusiveNameHolders
}

func attachSharedRecordEvidence(db *gorm.DB, rows []QueueVerdict, out map[int64]pairEvidence) error {
	ids := verdictWorkIDs(rows)
	if len(ids) == 0 {
		return nil
	}
	var found []struct {
		WorkID     int64  `gorm:"column:entity_id"`
		SourceID   int16  `gorm:"column:source_id"`
		SourceKey  string `gorm:"column:source_key"`
		ExternalID string `gorm:"column:external_id"`
		LinkKind   int16  `gorm:"column:link_kind"`
		MatchedBy  string `gorm:"column:matched_by"`
	}
	if err := db.Raw(`SELECT r.entity_id, r.source_id, cs.key AS source_key, r.external_id, r.link_kind, r.matched_by
		FROM catalog_external_ref r
		JOIN catalog_source cs ON cs.id = r.source_id
		JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
		WHERE r.entity_type = ? AND r.dead_at IS NULL AND r.source_id IN ? AND r.entity_id IN ?`,
		model.EntityTypeWork, identityRecordSourceIDs, ids).Scan(&found).Error; err != nil {
		return err
	}
	byWork := map[int64][]workIdentityRef{}
	wanted := map[identityRefKey]struct{}{}
	for _, r := range found {
		byWork[r.WorkID] = append(byWork[r.WorkID], workIdentityRef{
			WorkID: r.WorkID, SourceID: r.SourceID, SourceKey: r.SourceKey,
			ExternalID: r.ExternalID, LinkKind: r.LinkKind, MatchedBy: r.MatchedBy,
		})
	}
	for _, row := range rows {
		inA := map[identityRefKey]struct{}{}
		for _, r := range byWork[row.AID] {
			if identityBearingRef(r.LinkKind, r.MatchedBy) {
				inA[identityRefKey{r.SourceID, r.ExternalID}] = struct{}{}
			}
		}
		for _, r := range byWork[row.BID] {
			k := identityRefKey{r.SourceID, r.ExternalID}
			if _, ok := inA[k]; ok && identityBearingRef(r.LinkKind, r.MatchedBy) {
				wanted[k] = struct{}{}
			}
		}
	}
	holders := map[identityRefKey]int{}
	if err := loadIdentityHolders(db, wanted, holders); err != nil {
		return err
	}
	for _, row := range rows {
		sk, ext, n := pickSharedRecord(byWork[row.AID], byWork[row.BID], holders)
		if sk == "" {
			continue
		}
		ev := out[row.ID]
		ev.SharedSourceKey = sk
		ev.SharedExternalID = ext
		ev.SharedHolders = n
		out[row.ID] = ev
	}
	return nil
}

func loadIdentityHolders(db *gorm.DB, wanted map[identityRefKey]struct{}, out map[identityRefKey]int) error {
	if len(wanted) == 0 {
		return nil
	}
	bySrc := map[int16][]string{}
	for k := range wanted {
		bySrc[k.SourceID] = append(bySrc[k.SourceID], k.ExternalID)
	}
	for src, exts := range bySrc {
		for _, chunk := range chunkBy(exts, 500) {
			var found []struct {
				SourceID   int16  `gorm:"column:source_id"`
				ExternalID string `gorm:"column:external_id"`
				Holders    int    `gorm:"column:holders"`
			}
			if err := db.Raw(`SELECT r.source_id, r.external_id, count(DISTINCT r.entity_id) AS holders
				FROM catalog_external_ref r
				JOIN catalog_work w ON w.id = r.entity_id AND w.deleted_at IS NULL
				WHERE r.entity_type = ? AND r.dead_at IS NULL AND r.source_id = ? AND r.external_id IN ?
				GROUP BY 1, 2`, model.EntityTypeWork, src, chunk).Scan(&found).Error; err != nil {
				return err
			}
			for _, f := range found {
				out[identityRefKey{f.SourceID, f.ExternalID}] = f.Holders
			}
		}
	}
	return nil
}

func verdictWorkIDs(rows []QueueVerdict) []int64 {
	seen := map[int64]struct{}{}
	var ids []int64
	for _, r := range rows {
		for _, id := range []int64{r.AID, r.BID} {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

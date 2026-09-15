package llmsuggest

import (
	"strconv"
	"strings"
	"unicode"

	"api/internal/platform/catalog/model"

	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
)

// A confirm writes an exact identity and RejectRef is the only way back, so the
// bar has to be a fact and not a number. ref-v2 left it a number and the number
// stopped carrying anything: 650 of its 796 same verdicts came back at exactly
// 1.00, and the 0.9 gate that had passed 0 of ref-v1's rows passed 768 of
// these. Reading them showed confidence rose everywhere while evidence rose on
// about a third. Label 5771 Hades is a voice-drama circle matched onto the
// producer of The Fruit of Grisaia; 2428 SKY and 4002 PearMint are the same
// shape, a DLsite circle against a visual-novel brand sharing a short English
// name, all three at 1.00.
//
// So a same verdict confirms only when something other than the two names
// agrees. For a name-shaped entity that is the works it is attached to: the
// same upstream id on both sides, or failing that the same work title. For a
// work ref it is only the ids -- its titles are what the matching rule compared
// to queue the row in the first place, and re-reading the rule's own input is
// how ref-v1 came to top out at 0.85 on every one of 530 same verdicts.
type refEvidence struct {
	Corroborator string
	Unavailable  bool
}

func (e refEvidence) ok() bool { return e.Corroborator != "" }

func refCorroboration(db, eg *gorm.DB, reg sourceReg, rows []QueueVerdict) (map[int64]refEvidence, error) {
	out := map[int64]refEvidence{}
	var named, works []QueueVerdict
	for _, r := range rows {
		if r.Verdict != VerdictSame {
			continue
		}
		out[r.ID] = refEvidence{}
		switch r.EntityType {
		case model.EntityTypeLabel, model.EntityTypeCreditName, model.EntityTypeCharacter:
			named = append(named, r)
		case model.EntityTypeWork:
			works = append(works, r)
		}
	}
	if err := corroborateNamedRefs(db, eg, reg, named, out); err != nil {
		return nil, err
	}
	return out, corroborateWorkRefs(db, reg, works, out)
}

func corroborateNamedRefs(db, eg *gorm.DB, reg sourceReg, rows []QueueVerdict, out map[int64]refEvidence) error {
	if len(rows) == 0 {
		return nil
	}
	catWorks, err := catalogNeighbourWorks(db, rows)
	if err != nil {
		return err
	}
	srcWorks, srcTitles, unavailable, err := sourceNeighbourWorks(db, eg, reg, rows)
	if err != nil {
		return err
	}
	var flat []int64
	for _, ws := range catWorks {
		flat = append(flat, ws...)
	}
	refs, err := workSourceRefs(db, flat)
	if err != nil {
		return err
	}
	catTitles, err := workTitleKeys(db, flat)
	if err != nil {
		return err
	}

	for _, r := range rows {
		if unavailable[r.ID] {
			out[r.ID] = refEvidence{Unavailable: true}
			continue
		}
		mine := catWorks[r.ID]
		theirs := srcWorks[r.ID]
		if len(mine) == 0 || len(theirs) == 0 {
			continue
		}
		held := make(map[string]bool, len(theirs))
		for _, w := range theirs {
			held[w] = true
		}
		hit := ""
		for _, w := range mine {
			if id := refs[w][r.SourceID]; id != "" && held[id] {
				hit = reg.key(r.SourceID) + " " + id
				break
			}
		}
		if hit == "" {
			var wanted []string
			for _, w := range theirs {
				wanted = append(wanted, srcTitles[srcWorkKey(r.SourceID, w)]...)
			}
			var have []string
			for _, w := range mine {
				have = append(have, catTitles[w]...)
			}
			if k := firstTitleAgreement(have, wanted); k != "" {
				hit = "title " + k
			}
		}
		if hit != "" {
			out[r.ID] = refEvidence{Corroborator: hit}
		}
	}
	return nil
}

var catalogNeighbourWorkSQL = map[int16]string{
	model.EntityTypeLabel: `SELECT DISTINCT wl.label_id AS owner, wl.work_id AS work
		FROM catalog_work_label wl
		JOIN catalog_work w ON w.id = wl.work_id AND w.deleted_at IS NULL
		WHERE wl.label_id IN ?`,
	model.EntityTypeCreditName: `SELECT DISTINCT c.credit_name_id AS owner, c.work_id AS work
		FROM catalog_credit c
		JOIN catalog_work w ON w.id = c.work_id AND w.deleted_at IS NULL
		WHERE c.credit_name_id IN ?`,
	model.EntityTypeCharacter: `SELECT DISTINCT wc.character_id AS owner, wc.work_id AS work
		FROM catalog_work_character wc
		JOIN catalog_work w ON w.id = wc.work_id AND w.deleted_at IS NULL
		WHERE wc.character_id IN ?`,
}

func catalogNeighbourWorks(db *gorm.DB, rows []QueueVerdict) (map[int64][]int64, error) {
	byET := map[int16][]int64{}
	seen := map[string]bool{}
	for _, r := range rows {
		k := entityKey(r.EntityType, r.EntityID)
		if seen[k] {
			continue
		}
		seen[k] = true
		byET[r.EntityType] = append(byET[r.EntityType], r.EntityID)
	}
	owner := map[string][]int64{}
	for et, ids := range byET {
		sql, ok := catalogNeighbourWorkSQL[et]
		if !ok {
			continue
		}
		for _, chunk := range chunkBy(ids, 500) {
			var got []struct {
				Owner int64 `gorm:"column:owner"`
				Work  int64 `gorm:"column:work"`
			}
			if err := db.Raw(sql, chunk).Scan(&got).Error; err != nil {
				return nil, err
			}
			for _, g := range got {
				k := entityKey(et, g.Owner)
				owner[k] = append(owner[k], g.Work)
			}
		}
	}
	out := map[int64][]int64{}
	for _, r := range rows {
		out[r.ID] = owner[entityKey(r.EntityType, r.EntityID)]
	}
	return out, nil
}

func workSourceRefs(db *gorm.DB, works []int64) (map[int64]map[int16]string, error) {
	out := map[int64]map[int16]string{}
	for _, chunk := range chunkBy(dedupeIDs(works), 500) {
		var got []struct {
			Work int64  `gorm:"column:entity_id"`
			Src  int16  `gorm:"column:source_id"`
			Ext  string `gorm:"column:external_id"`
		}
		if err := db.Raw(`SELECT entity_id, source_id, external_id FROM catalog_external_ref
			WHERE entity_type = ? AND link_kind = ? AND dead_at IS NULL AND entity_id IN ?`,
			model.EntityTypeWork, model.LinkKindExact, chunk).Scan(&got).Error; err != nil {
			return nil, err
		}
		for _, g := range got {
			if out[g.Work] == nil {
				out[g.Work] = map[int16]string{}
			}
			out[g.Work][g.Src] = g.Ext
		}
	}
	return out, nil
}

func workTitleKeys(db *gorm.DB, works []int64) (map[int64][]string, error) {
	out := map[int64][]string{}
	add := func(id int64, title string) {
		k := titleKey(title)
		if k == "" {
			return
		}
		out[id] = append(out[id], k)
	}
	for _, chunk := range chunkBy(dedupeIDs(works), 500) {
		var titles []struct {
			Work  int64  `gorm:"column:work_id"`
			Title string `gorm:"column:title"`
		}
		if err := db.Raw(`SELECT work_id, title FROM catalog_work_title WHERE work_id IN ? AND title <> ''`,
			chunk).Scan(&titles).Error; err != nil {
			return nil, err
		}
		for _, t := range titles {
			add(t.Work, t.Title)
		}
		var names []struct {
			ID   int64  `gorm:"column:id"`
			Name string `gorm:"column:display_name"`
		}
		if err := db.Raw(`SELECT id, display_name FROM catalog_work WHERE id IN ? AND display_name <> ''`,
			chunk).Scan(&names).Error; err != nil {
			return nil, err
		}
		for _, n := range names {
			add(n.ID, n.Name)
		}
	}
	return out, nil
}

const (
	titleAgreeExact  = 3
	titleAgreePrefix = 4
)

// titleKey folds away everything two registries disagree about while spelling
// the same work: width, case, and the brackets and tildes that decorate a
// Japanese title. NFKC is what makes ＤＥＡＲ and DEAR one string.
func titleKey(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKC.String(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// Prefix containment, not equality. 海の唄がきこえる and 海の唄がきこえる 上巻
// are one work split into volumes, and Clear－クリア－ is the short form of
// Clear -クリア- 新しい風の吹く丘で. Scoring those as disjoint moved 80 rows of
// a 768-row census into "held for a human" who has nothing to add to them.
func titlesAgree(a, b string) bool {
	if a == b {
		return len([]rune(a)) >= titleAgreeExact
	}
	if len([]rune(a)) < titleAgreePrefix || len([]rune(b)) < titleAgreePrefix {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func firstTitleAgreement(have, wanted []string) string {
	if len(have) == 0 || len(wanted) == 0 {
		return ""
	}
	exact := make(map[string]bool, len(wanted))
	for _, w := range wanted {
		exact[w] = true
	}
	for _, h := range have {
		if len([]rune(h)) >= titleAgreeExact && exact[h] {
			return h
		}
	}
	for _, h := range have {
		for _, w := range wanted {
			if titlesAgree(h, w) {
				return h
			}
		}
	}
	return ""
}

func srcWorkKey(sourceID int16, work string) string {
	return strconv.FormatInt(int64(sourceID), 10) + ":" + work
}

func dedupeIDs(in []int64) []int64 {
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

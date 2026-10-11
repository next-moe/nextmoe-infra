package service

import (
	"context"
	"fmt"
	"strings"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// nameFoldSQL folds a credit or person name the way two sources spell one
// person on one work: ErogameScape writes 木村貴宏(すがわらあわじ、すがわらあわぢ)
// where VNDB writes 木村 貴宏. The parenthesised alias list, whitespace and
// case are what differ.
func nameFoldSQL(expr string) string {
	return fmt.Sprintf(`lower(regexp_replace(regexp_replace(normalize(%s, NFKC), '\([^)]*\)', '', 'g'), '\s', '', 'g'))`, expr)
}

const (
	minNameFoldLen = 2
	// A fold shorter than this can name two people on one work by chance, so
	// one shared work does not settle it. The 2026-10-11 dry run attached
	// さくら(声優) to the lyricalmagical person over a single work.
	soleWorkMinFoldLen = 4
)

func strongCoCreditSQL(works, foldLen string) string {
	return fmt.Sprintf(`(%s >= 2 OR %s >= %d)`, works, foldLen, soleWorkMinFoldLen)
}

// coCreditHostsSQL finds the people already holding a name that folds to one of
// the given names and is credited on a work those names are credited on. Two
// spellings of one folded name on one work's staff list are one person.
var coCreditHostsSQL = `SELECT held.person_id,
		` + strongCoCreditSQL("count(DISTINCT c.work_id)", "max(char_length("+nameFoldSQL("n.name")+"))") + ` AS strong
	FROM catalog_credit_name n
	JOIN catalog_credit c ON c.credit_name_id = n.id
	JOIN catalog_credit hc ON hc.work_id = c.work_id
	JOIN catalog_credit_name held ON held.id = hc.credit_name_id AND held.person_id IS NOT NULL
	WHERE n.id IN ?
	  AND char_length(` + nameFoldSQL("n.name") + `) >= ` + fmt.Sprint(minNameFoldLen) + `
	  AND ` + nameFoldSQL("held.name") + ` = ` + nameFoldSQL("n.name") + `
	  AND ` + editspec.NotSuppressedCreditSQL("c") + `
	  AND ` + editspec.NotSuppressedCreditSQL("hc") + `
	GROUP BY held.person_id
	ORDER BY held.person_id`

type coCreditHost struct {
	PersonID int64
	Strong   bool
}

func coCreditHosts(tx *gorm.DB, creditNameIDs []int64) ([]coCreditHost, error) {
	var hosts []coCreditHost
	if err := tx.Raw(coCreditHostsSQL, creditNameIDs).Scan(&hosts).Error; err != nil {
		return nil, fmt.Errorf("co-credit hosts of %v: %w", creditNameIDs, err)
	}
	return hosts, nil
}

// namedCTEs folds every credit name once. homonym holds the folds one source
// lists under two different names: ErogameScape keeps namesakes apart with a
// parenthesised tag — 紫陽花(男性) and 紫陽花(女性), URA(ぷにあつ) and
// URA(AC-Promenade URA) — and the fold erases it; the 2026-10-11 dry run put
// both of each pair on one person.
var namedCTEs = fmt.Sprintf(`named AS MATERIALIZED (
			SELECT n.id, n.person_id, n.name, %s AS f FROM catalog_credit_name n
		), homonym AS MATERIALIZED (
			SELECT x.f FROM named x
			JOIN catalog_external_ref r ON r.entity_type = %d AND r.entity_id = x.id AND r.link_kind = %d
			GROUP BY x.f, r.source_id HAVING count(DISTINCT x.id) > 1
		)`, nameFoldSQL("n.name"), model.EntityTypeCreditName, model.LinkKindExact)

type CoCreditAttach struct {
	CreditNameID int64
	Name         string
	PersonID     int64
	Hosts        int
	Works        int
	Strong       bool
}

// CoCreditAttachPlan lists every name held by no person that a person already
// holds under the same fold on a shared work. Hosts > 1 means the fold is
// ambiguous on that name's works; a plan that is not Strong rests on one work
// under a short fold. Neither may be attached.
func (s *LinkService) CoCreditAttachPlan(ctx context.Context) ([]CoCreditAttach, error) {
	var rows []CoCreditAttach
	err := s.db.WithContext(ctx).Raw(`
		WITH `+namedCTEs+`, hit AS (
			SELECT o.id, o.name, h.person_id, count(DISTINCT c.work_id) AS works, char_length(o.f) AS flen
			FROM named o
			JOIN catalog_credit c ON c.credit_name_id = o.id
			JOIN catalog_credit hc ON hc.work_id = c.work_id
			JOIN named h ON h.id = hc.credit_name_id AND h.person_id IS NOT NULL AND h.f = o.f
			WHERE o.person_id IS NULL AND char_length(o.f) >= ?
			  AND NOT EXISTS (SELECT 1 FROM homonym hm WHERE hm.f = o.f)
			  AND `+editspec.NotSuppressedCreditSQL("c")+`
			  AND `+editspec.NotSuppressedCreditSQL("hc")+`
			GROUP BY o.id, o.name, h.person_id, o.f
		)
		SELECT id AS credit_name_id, name, min(person_id) AS person_id,
		       count(*) AS hosts, sum(works) AS works, bool_and(`+strongCoCreditSQL("works", "flen")+`) AS strong
		FROM hit GROUP BY id, name ORDER BY id`, minNameFoldLen).Scan(&rows).Error
	return rows, err
}

// AttachName links one orphan name to a person found by CoCreditAttachPlan. A
// name some other lane linked in the meantime is left alone.
func (s *LinkService) AttachName(ctx context.Context, creditNameID, personID int64, actorID *int64) (bool, error) {
	attached := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		n, err := lockCreditName(tx, creditNameID)
		if err != nil {
			return err
		}
		if n.PersonID != nil {
			return nil
		}
		if _, err := s.attachOrphan(tx, personID, n, actorID); err != nil {
			return err
		}
		attached = true
		return nil
	})
	return attached, err
}

type CoCreditPersonPair struct {
	AID, BID       int64
	AName, BName   string
	Works          int
	SameDisplay    bool
	RefConflict    bool
	AHasVNDB       bool
	BHasVNDB       bool
	ACredits       int
	BCredits       int
	ProposalExists bool
}

// SurvivorTarget keeps the person VNDB anchors, then the one with more credits,
// then the older one.
func (p CoCreditPersonPair) SurvivorTarget() (source, target int64) {
	switch {
	case p.AHasVNDB != p.BHasVNDB:
		if p.AHasVNDB {
			return p.BID, p.AID
		}
		return p.AID, p.BID
	case p.ACredits != p.BCredits:
		if p.ACredits > p.BCredits {
			return p.BID, p.AID
		}
		return p.AID, p.BID
	default:
		return p.BID, p.AID
	}
}

// CoCreditPersonPairs lists live people who each hold a name of the same fold
// credited on a shared work. SameDisplay marks the pairs whose own display
// names fold equal too; RefConflict marks pairs an identity registry anchors to
// two different people.
func (s *LinkService) CoCreditPersonPairs(ctx context.Context) ([]CoCreditPersonPair, error) {
	var rows []CoCreditPersonPair
	err := s.db.WithContext(ctx).Raw(`
		WITH `+namedCTEs+`, held AS (
			SELECT named.* FROM named
			JOIN catalog_person p ON p.id = named.person_id AND p.deleted_at IS NULL
		), pair AS (
			SELECT a.person_id AS a_id, b.person_id AS b_id, count(DISTINCT ca.work_id) AS works
			FROM held a
			JOIN held b ON b.f = a.f AND b.person_id > a.person_id
			JOIN catalog_credit ca ON ca.credit_name_id = a.id
			JOIN catalog_credit cb ON cb.credit_name_id = b.id AND cb.work_id = ca.work_id
			WHERE char_length(a.f) >= ?
			  AND NOT EXISTS (SELECT 1 FROM homonym hm WHERE hm.f = a.f)
			  AND `+editspec.NotSuppressedCreditSQL("ca")+`
			  AND `+editspec.NotSuppressedCreditSQL("cb")+`
			GROUP BY 1, 2
			HAVING `+strongCoCreditSQL("count(DISTINCT ca.work_id)", "max(char_length(a.f))")+`
		)
		SELECT pair.a_id, pair.b_id, pa.display_name AS a_name, pb.display_name AS b_name, pair.works,
		       `+nameFoldSQL("pa.display_name")+` = `+nameFoldSQL("pb.display_name")+` AS same_display,
		       EXISTS (SELECT 1 FROM catalog_external_ref ra
		               JOIN catalog_external_ref rb ON rb.entity_type = ra.entity_type AND rb.source_id = ra.source_id
		                AND rb.link_kind = ra.link_kind AND rb.external_id <> ra.external_id
		               WHERE ra.entity_type = ? AND ra.link_kind = ? AND ra.source_id NOT IN ?
		                 AND ra.entity_id = pair.a_id AND rb.entity_id = pair.b_id) AS ref_conflict,
		       EXISTS (SELECT 1 FROM catalog_external_ref r WHERE r.entity_type = ? AND r.entity_id = pair.a_id
		                AND r.source_id = ? AND r.link_kind = ?) AS a_has_vndb,
		       EXISTS (SELECT 1 FROM catalog_external_ref r WHERE r.entity_type = ? AND r.entity_id = pair.b_id
		                AND r.source_id = ? AND r.link_kind = ?) AS b_has_vndb,
		       (SELECT count(*) FROM catalog_credit c JOIN catalog_credit_name n ON n.id = c.credit_name_id
		         WHERE n.person_id = pair.a_id) AS a_credits,
		       (SELECT count(*) FROM catalog_credit c JOIN catalog_credit_name n ON n.id = c.credit_name_id
		         WHERE n.person_id = pair.b_id) AS b_credits,
		       EXISTS (SELECT 1 FROM catalog_merge_proposal mp WHERE mp.entity_type = ?
		                AND ((mp.source_entity_id = pair.a_id AND mp.target_entity_id = pair.b_id)
		                  OR (mp.source_entity_id = pair.b_id AND mp.target_entity_id = pair.a_id))) AS proposal_exists
		FROM pair
		JOIN catalog_person pa ON pa.id = pair.a_id
		JOIN catalog_person pb ON pb.id = pair.b_id
		ORDER BY pair.a_id, pair.b_id`,
		minNameFoldLen,
		model.EntityTypePerson, model.LinkKindExact, model.IdentityVetoExemptSourceIDsFor(model.EntityTypePerson),
		model.EntityTypePerson, model.SourceVNDB, model.LinkKindExact,
		model.EntityTypePerson, model.SourceVNDB, model.LinkKindExact,
		model.EntityTypePerson).Scan(&rows).Error
	return rows, err
}

func (p CoCreditPersonPair) String() string {
	return fmt.Sprintf("P%d %q ~ P%d %q works=%d", p.AID, strings.TrimSpace(p.AName), p.BID, strings.TrimSpace(p.BName), p.Works)
}

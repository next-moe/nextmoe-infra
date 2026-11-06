package service

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WorkIdentityKeySourceIDs are the sources whose ids name a game or a product
// of one, so two works holding the same id are a pair to decide.
// WorkRegistrySourceIDs are the subset whose id is the game itself: sharing one
// is the identity. The rest are storefront SKUs and playtime entries, which a
// bundle or a fan disc sold with its parent legitimately shares: on 2026-11-06,
// 466 work pairs held one DMM or Steam id between EG's work-level ref and
// VNDB's release-level ref, and the sample was fan discs, append discs, packs
// and free all-ages cuts of the parent game, not the parent twice.
var (
	WorkRegistrySourceIDs = []int16{
		model.SourceVNDB, model.SourceBangumi, model.SourceErogameScape,
		model.SourceAniList, model.SourceMAL, model.SourceCurated,
	}
	WorkIdentityKeySourceIDs = append(slices.Clone(WorkRegistrySourceIDs),
		model.SourceDLsite, model.SourceSteam, model.SourceDMM, model.SourceGetchu, model.SourceHowLongToBeat)
)

func Int16CSV(ids []int16) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(int(id))
	}
	return strings.Join(parts, ",")
}

// WorkIdentityKeysSQL projects every identity key onto the work that holds it
// as (work_id, source_id, k): a work's own refs and the refs on its live
// releases, exact or probable, lower-cased. Reading one level, or one case,
// was the hole every time: 6253/217311 sat for months on dmm hobc_0507 held by
// a release on one side and the work on the other, and 224163 held Steam
// 3919400 as a probable work ref while 54607's release held it exact. VNDB
// release ids stay out: a VNDB release naming several VNs is copied onto a
// release of each, so the id is a bundle, not a game.
func WorkIdentityKeysSQL() string {
	return `SELECT DISTINCT coalesce(rel.work_id, r.entity_id) AS work_id, r.source_id, lower(r.external_id) AS k
		FROM catalog_external_ref r
		LEFT JOIN catalog_release rel ON r.entity_type = ` + strconv.Itoa(int(model.EntityTypeRelease)) + `
			AND rel.id = r.entity_id AND rel.deleted_at IS NULL
		WHERE r.dead_at IS NULL AND r.link_kind IN (` + Int16CSV([]int16{model.LinkKindExact, model.LinkKindProbable}) + `)
		  AND r.source_id IN (` + Int16CSV(WorkIdentityKeySourceIDs) + `)
		  AND (r.entity_type = ` + strconv.Itoa(int(model.EntityTypeWork)) + `
		       OR (r.entity_type = ` + strconv.Itoa(int(model.EntityTypeRelease)) + ` AND rel.id IS NOT NULL
		           AND r.source_id <> ` + strconv.Itoa(int(model.SourceVNDB)) + `))`
}

type IdentityKey struct {
	SourceID   int16
	ExternalID string
}

// MintedWork is a work an import lane has just created, with any identity key
// the lane knows from its source record but has not written as a ref.
type MintedWork struct {
	ID   int64
	Keys []IdentityKey
}

type MintScreenHit struct {
	WorkID int64 `gorm:"column:a"`
	Other  int64 `gorm:"column:b"`
	ByName bool  `gorm:"column:by_name"`
}

// ScreenMintedWorks is the last step of every import mint: a new work that
// shares an official name (however short) or an identity key with a live work
// of its medium is quarantined, and the pair filed for the census and
// the judge. Each lane decides attach-or-mint by its own source-specific
// matching first; this screen is what keeps a lane's blind spot from going
// live. Bangumi 589607 minted 224163 live beside 54607 (黄莓C) because its
// lane compared names of four runes or more only, and forum built a published
// page on the twin before any detector saw the pair. A disagreeing exact id
// under WorkIdentityVetoSourceIDs proves the two distinct and is no hit, and
// so is a catalog_match_rejection saying one of the mint's records is not the
// other work.
func ScreenMintedWorks(tx *gorm.DB, works []MintedWork) ([]MintScreenHit, error) {
	if len(works) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(works))
	var extraVals []string
	var args []any
	for i, w := range works {
		ids[i] = w.ID
		for _, k := range w.Keys {
			if !slices.Contains(WorkIdentityKeySourceIDs, k.SourceID) || strings.TrimSpace(k.ExternalID) == "" {
				continue
			}
			extraVals = append(extraVals, "(CAST(? AS bigint), CAST(? AS smallint), lower(CAST(? AS text)))")
			args = append(args, w.ID, k.SourceID, k.ExternalID)
		}
	}
	extra := `SELECT NULL::bigint AS work_id, NULL::smallint AS source_id, NULL::text AS k WHERE false`
	if len(extraVals) > 0 {
		extra = `SELECT * FROM (VALUES ` + strings.Join(extraVals, ", ") + `) v(work_id, source_id, k)`
	}
	args = append(args, ids)
	var hits []MintScreenHit
	err := tx.Raw(`
		WITH extra AS (`+extra+`),
		mine AS (SELECT id, medium_id FROM catalog_work WHERE id IN ? AND deleted_at IS NULL),
		corpus AS (`+WorkDupeCorpusSQL()+`),
		title_hits AS (
			SELECT mn.work_id AS a, o.work_id AS b
			FROM corpus mn
			JOIN mine m ON m.id = mn.work_id
			JOIN corpus o ON o.n = mn.n AND o.work_id <> mn.work_id
			WHERE mn.long OR (mn.official AND o.official)
		),
		keys AS (`+WorkIdentityKeysSQL()+`),
		mykeys AS (
			SELECT k.work_id, k.source_id, k.k FROM keys k JOIN mine m ON m.id = k.work_id
			UNION
			SELECT work_id, source_id, k FROM extra
		),
		key_hits AS (
			SELECT mk.work_id AS a, o.work_id AS b
			FROM mykeys mk JOIN keys o ON o.source_id = mk.source_id AND o.k = mk.k AND o.work_id <> mk.work_id
		),
		veto AS (
			SELECT entity_id AS work_id, source_id, external_id FROM catalog_external_ref
			WHERE entity_type = `+strconv.Itoa(int(model.EntityTypeWork))+` AND link_kind = `+strconv.Itoa(int(model.LinkKindExact))+`
			  AND dead_at IS NULL AND source_id IN (`+Int16CSV(model.WorkIdentityVetoSourceIDs)+`)
		),
		hits AS (
			SELECT a, b, true AS by_name FROM title_hits
			UNION ALL
			SELECT a, b, false FROM key_hits
		)
		SELECT h.a, h.b, bool_or(h.by_name) AS by_name
		FROM hits h
		JOIN mine m ON m.id = h.a
		JOIN catalog_work o ON o.id = h.b AND o.deleted_at IS NULL AND o.medium_id = m.medium_id
		  AND o.status IN (`+Int16CSV([]int16{model.WorkStatusLive, model.WorkStatusStub})+`)
		WHERE NOT EXISTS (
			SELECT 1 FROM veto va JOIN veto vb ON vb.source_id = va.source_id AND vb.external_id <> va.external_id
			WHERE va.work_id = h.a AND vb.work_id = h.b)
		  AND NOT EXISTS (
			SELECT 1 FROM catalog_match_rejection mr
			JOIN catalog_external_ref ar ON ar.source_id = mr.source_id AND ar.external_id = mr.external_id
			LEFT JOIN catalog_release arel ON ar.entity_type = `+strconv.Itoa(int(model.EntityTypeRelease))+` AND arel.id = ar.entity_id
			WHERE mr.entity_type = `+strconv.Itoa(int(model.EntityTypeWork))+` AND mr.entity_id = h.b
			  AND ((ar.entity_type = `+strconv.Itoa(int(model.EntityTypeWork))+` AND ar.entity_id = h.a)
			    OR (ar.entity_type = `+strconv.Itoa(int(model.EntityTypeRelease))+` AND arel.work_id = h.a)))
		GROUP BY h.a, h.b
		ORDER BY h.a, h.b`, args...).Scan(&hits).Error
	if err != nil || len(hits) == 0 {
		return hits, err
	}
	hits = capScreenHits(hits)

	quarantine := ScreenedWorkIDs(hits)
	cands := make([]model.CatalogMatchCandidate, 0, len(hits))
	for _, h := range hits {
		reason := model.CandidateReasonSharedExternalID
		if h.ByName {
			reason = model.CandidateReasonNameNormEqual
		}
		cands = append(cands, model.CatalogMatchCandidate{
			EntityType: model.EntityTypeWork, AID: min(h.WorkID, h.Other), BID: max(h.WorkID, h.Other),
			Reason: reason, Status: model.CandidateStatusPending,
		})
	}
	if err := tx.Exec(`UPDATE catalog_work SET status = ?, updated_at = now() WHERE id IN ? AND status = ?`,
		model.WorkStatusQuarantine, quarantine, model.WorkStatusLive).Error; err != nil {
		return nil, fmt.Errorf("quarantine screened works: %w", err)
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(cands, 1000).Error; err != nil {
		return nil, fmt.Errorf("file screened pairs: %w", err)
	}
	return hits, nil
}

// maxScreenCandidates is the import lanes' own candidate cap: a generic name
// that a dozen works answer to quarantines the mint once, and the judge needs
// a few of the holders to decide it, not all of them.
const maxScreenCandidates = 3

func capScreenHits(hits []MintScreenHit) []MintScreenHit {
	out := hits[:0]
	n := 0
	for i, h := range hits {
		if i > 0 && hits[i-1].WorkID != h.WorkID {
			n = 0
		}
		if n < maxScreenCandidates {
			out = append(out, h)
		}
		n++
	}
	return out
}

// ScreenedWorkIDs lists the minted works a screen quarantined, once each.
func ScreenedWorkIDs(hits []MintScreenHit) []int64 {
	var out []int64
	for _, h := range hits {
		if len(out) == 0 || out[len(out)-1] != h.WorkID {
			out = append(out, h.WorkID)
		}
	}
	return out
}

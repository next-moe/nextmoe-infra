package importer

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// A prod --apply died on a 23505 because the resume key (work_id, rid) is FINER
// than the unique it protects (uq_catalog_external_ref_exact, which carries no
// work column): it honestly reported "this work has not done rid yet", the wave
// planned a mint, and the mint hit the anchor a DIFFERENT work already held.
// catalog_external_ref is therefore the single identity index for releases and
// Extra.vndb_id is payload, never the authority. The row decision is keyed
// (work_id, rid) and is liveness-BLIND — a retired row is not a vacancy, it
// still answers to its id — while the anchor decision is keyed by rid alone.

const (
	ruleVNDBRelease         = "rule:vndb-release-import"
	ruleVNDBReleaseProbable = "rule:vndb-release-import-probable"
	ruleVNDBReleaseBackfill = "rule:vndb-release-backfill"
)

const releaseApplyBatch = 500

type ReleaseStats struct {
	InGatePairs         int
	Planned             int
	ProbableBackfilled  int
	ReleasesWritten     int
	AnchorsWritten      int
	ProbableRefsWritten int
	MultiVNUnanchored   int
	AnchorHeldByOther   int
	StaleAnchorHolders  int
	AnchorRaceLost      int
	SkippedExisting     int
	SkippedRetired      int
	KindDefault         int
	KindTrial           int
	KindPatch           int
	NoDate              int
	NoTitle             int
	BatchFailures       int
	Errors              int
}

type releaseGateRow struct {
	WorkID int64  `gorm:"column:work_id"`
	VID    string `gorm:"column:vid"`
	RID    string `gorm:"column:rid"`
	RType  string `gorm:"column:rtype"`
}

type anchorDecision int8

const (
	anchorMint anchorDecision = iota
	anchorHeldByOther
	anchorMultiVN
)

type releasePlan struct {
	rel    model.CatalogRelease
	rid    string
	anchor anchorDecision
}

func (im *Importer) RunReleases() (ReleaseStats, error) {
	var st ReleaseStats

	backfilled, err := im.backfillReleaseProbableRefs()
	if err != nil {
		return st, err
	}
	st.ProbableBackfilled = backfilled

	var gates []releaseGateRow
	if err := im.catalog.Raw(`
		SELECT r.entity_id AS work_id, min(r.external_id) AS vid, rv.id AS rid, min(rv.rtype) AS rtype
		FROM src_vndb.releases_vn rv
		JOIN catalog_external_ref r ON r.entity_type = ? AND r.source_id = ? AND r.link_kind = ? AND r.external_id = rv.vid
		GROUP BY r.entity_id, rv.id`,
		model.EntityTypeWork, vndbSource, model.LinkKindExact).Scan(&gates).Error; err != nil {
		return st, err
	}
	if im.limit > 0 {
		gates = capReleaseGatesByWork(gates, im.limit)
	}
	st.InGatePairs = len(gates)

	meta, err := im.loadReleaseMeta()
	if err != nil {
		return st, err
	}
	platforms, err := im.loadReleasePlatforms()
	if err != nil {
		return st, err
	}
	titles, err := im.loadReleaseTitles()
	if err != nil {
		return st, err
	}
	vnCount, err := im.loadReleaseVNCounts()
	if err != nil {
		return st, err
	}
	existing, err := im.loadExistingVNDBReleaseKeys()
	if err != nil {
		return st, err
	}
	exactHolders, err := im.loadReleaseExactHolders()
	if err != nil {
		return st, err
	}

	maxYear := time.Now().Year() + 3
	var plans []releasePlan
	var stale []staleAnchorRow
	for _, g := range gates {
		m, ok := meta[g.RID]
		if !ok {
			st.Errors++
			continue
		}
		if retired, held := existing[releaseKey(g.WorkID, g.RID)]; held {
			if retired {
				st.SkippedRetired++
			} else {
				st.SkippedExisting++
			}
			continue
		}

		rel := model.CatalogRelease{WorkID: g.WorkID, Extra: datatypes.JSON(`{}`)}

		switch {
		case m.patch:
			rel.Kind = model.ReleaseKindPatch
			st.KindPatch++
		case g.RType == "trial":
			rel.Kind = model.ReleaseKindTrial
			st.KindTrial++
		default:
			rel.Kind = model.ReleaseKindDefault
			st.KindDefault++
		}

		if strings.TrimSpace(m.olang) != "" {
			lang := m.olang
			rel.Lang = &lang
		}
		if title, ok := originalTitle(titles[g.RID], m.olang); ok {
			rel.Title = &title
		} else {
			st.NoTitle++
		}

		plats := platforms[g.RID]
		if len(plats) > 0 {
			first := plats[0]
			rel.Platform = &first
		}

		if y, mo, d, ok := parseVNDBReleased(m.released, maxYear); ok {
			rel.ReleasedY = &y
			rel.ReleasedM = mo
			rel.ReleasedD = d
		} else {
			st.NoDate++
		}

		rel.Extra = buildReleaseExtra(g.RID, m, langSet(titles[g.RID]), plats)

		anchor := anchorMint
		switch holder, held := exactHolders[g.RID]; {
		case vnCount[g.RID] != 1:
			anchor = anchorMultiVN
			st.MultiVNUnanchored++
		case held:
			anchor = anchorHeldByOther
			st.AnchorHeldByOther++
			if holder.workID != g.WorkID {
				st.StaleAnchorHolders++
				stale = append(stale, staleAnchorRow{
					rid: g.RID, gateWorkID: g.WorkID, gateWorkVID: g.VID,
					holderReleaseID: holder.releaseID, holderWorkID: holder.workID,
					holderRetired: holder.retired,
				})
			}
		}
		plans = append(plans, releasePlan{rel: rel, rid: g.RID, anchor: anchor})
	}
	st.Planned = len(plans)

	anchorable := 0
	for _, p := range plans {
		if p.anchor == anchorMint {
			anchorable++
		}
	}

	slog.Info("vndb releases plan",
		"in_gate_pairs", st.InGatePairs, "planned", st.Planned, "skipped_existing", st.SkippedExisting,
		"skipped_retired", st.SkippedRetired, "probable_backfilled", st.ProbableBackfilled,
		"kind_default", st.KindDefault, "kind_trial", st.KindTrial, "kind_patch", st.KindPatch,
		"anchors_planned", anchorable, "multi_vn_unanchored", st.MultiVNUnanchored,
		"anchor_held_by_other", st.AnchorHeldByOther, "stale_anchor_holders", st.StaleAnchorHolders,
		"no_date", st.NoDate, "no_title", st.NoTitle, "errors", st.Errors)

	if im.staleAnchorsOut != "" {
		if err := im.exportStaleAnchors(stale, im.staleAnchorsOut); err != nil {
			return st, err
		}
	}

	if im.dryRun {
		st.ReleasesWritten = len(plans)
		st.AnchorsWritten = anchorable
		st.ProbableRefsWritten = len(plans) - anchorable
		return st, nil
	}
	if len(plans) == 0 {
		return st, nil
	}

	err = im.catalog.Transaction(func(tx *gorm.DB) error {
		for start := 0; start < len(plans); start += releaseApplyBatch {
			end := min(start+releaseApplyBatch, len(plans))
			if err := applyReleaseBatch(tx, plans[start:end], &st); err != nil {
				return err
			}
		}
		return nil
	})
	return st, err
}

func applyReleaseBatch(tx *gorm.DB, plans []releasePlan, st *ReleaseStats) error {
	const sp = "vndb_releases_batch"
	if err := tx.SavePoint(sp).Error; err != nil {
		return err
	}
	var delta ReleaseStats
	if err := writeReleaseBatch(tx, plans, &delta); err != nil {
		if rbErr := tx.RollbackTo(sp).Error; rbErr != nil {
			return rbErr
		}
		st.BatchFailures++
		st.Errors++
		slog.Error("vndb releases batch rolled back",
			"rows", len(plans), "first_rid", plans[0].rid, "last_rid", plans[len(plans)-1].rid, "error", err)
		return nil
	}
	if err := tx.Exec("RELEASE SAVEPOINT " + sp).Error; err != nil {
		return err
	}
	st.ReleasesWritten += delta.ReleasesWritten
	st.AnchorsWritten += delta.AnchorsWritten
	st.ProbableRefsWritten += delta.ProbableRefsWritten
	st.AnchorRaceLost += delta.AnchorRaceLost
	return nil
}

func writeReleaseBatch(tx *gorm.DB, plans []releasePlan, st *ReleaseStats) error {
	releases := make([]model.CatalogRelease, len(plans))
	for i, p := range plans {
		releases[i] = p.rel
	}
	if err := tx.CreateInBatches(releases, 1000).Error; err != nil {
		return err
	}

	var mints, probables []releaseAnchorItem
	revs := make([]model.CatalogRevision, len(releases))
	for i := range releases {
		revs[i] = importedRev(model.EntityTypeRelease, releases[i].ID, releaseSnapshotJSON(releases[i]))
		item := releaseAnchorItem{releaseID: releases[i].ID, rid: plans[i].rid}
		if plans[i].anchor == anchorMint {
			mints = append(mints, item)
		} else {
			probables = append(probables, item)
		}
	}

	landed, err := mintReleaseExactAnchors(tx, mints)
	if err != nil {
		return err
	}
	for _, m := range mints {
		if !landed[m.releaseID] {
			st.AnchorRaceLost++
			probables = append(probables, m)
		}
	}
	st.AnchorsWritten = len(landed)

	written, err := insertReleaseProbableRefs(tx, probables, ruleVNDBReleaseProbable)
	if err != nil {
		return err
	}
	st.ProbableRefsWritten = written

	if err := tx.CreateInBatches(revs, 1000).Error; err != nil {
		return err
	}
	st.ReleasesWritten = len(releases)

	hosts := make([]int64, 0, len(releases))
	for _, r := range releases {
		hosts = append(hosts, r.WorkID)
	}
	return touchWorks(tx, hosts)
}

func releaseKey(workID int64, vndbID string) string {
	return strconv.FormatInt(workID, 10) + "|" + vndbID
}

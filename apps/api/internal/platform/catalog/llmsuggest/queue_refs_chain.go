package llmsuggest

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// StagingDBs are the upstream mirrors the chain verifier joins against. Each
// is a database on the same postgres server as the catalog, reached by swapping
// the dbname on the catalog credentials, so a nil field means the connection
// failed — not that the family has no evidence.
type StagingDBs struct {
	EG   *gorm.DB
	HLTB *gorm.DB
}

type chainStep struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type chainResult struct {
	Verdict    string
	Reason     string
	Confidence float64
	Evidence   any
}

func runChainLane(ctx context.Context, db *gorm.DB, up StagingDBs, reg sourceReg, work []refItem, queue string, conc int, nJudged, nErrs *atomic.Int64) (int, int) {
	if len(work) == 0 {
		return 0, 0
	}
	results, err := verifyChainBatch(db, up, reg, work)
	startJudged, startErrs := nJudged.Load(), nErrs.Load()
	if err != nil {
		runPool(ctx, work, conc, func(_ context.Context, it refItem) {
			row := chainRow(it, queue)
			row.Error = truncate(err.Error(), 500)
			nErrs.Add(1)
			persistQueueVerdict(db, &row)
		})
		return int(nJudged.Load() - startJudged), int(nErrs.Load() - startErrs)
	}
	runPool(ctx, work, conc, func(_ context.Context, it refItem) {
		row := chainRow(it, queue)
		v := results[it.Hash]
		if v.Verdict == "" {
			msg := v.Reason
			if msg == "" {
				msg = "chain verifier produced no result"
			}
			row.Error = truncate(msg, 500)
			nErrs.Add(1)
		} else {
			row.Verdict, row.Reason, row.Confidence = v.Verdict, v.Reason, v.Confidence
			row.Evidence = evidenceJSON(v.Evidence)
			nJudged.Add(1)
		}
		persistQueueVerdict(db, &row)
	})
	return int(nJudged.Load() - startJudged), int(nErrs.Load() - startErrs)
}

func chainRow(it refItem, queue string) QueueVerdict {
	return QueueVerdict{
		Queue: queue, Lane: LaneChain,
		EntityType: it.EntityType, EntityID: it.EntityID, SourceID: it.SourceID, ExternalID: it.ExternalID,
		InputHash: it.Hash, Model: ChainModel, PromptVersion: PromptChainV2,
	}
}

func verifyChainBatch(db *gorm.DB, up StagingDBs, reg sourceReg, items []refItem) (map[string]chainResult, error) {
	out := map[string]chainResult{}
	var vndbRel, egDMM, egSteam, hltbSteam []refItem
	for _, it := range items {
		switch it.MatchedBy {
		case matchedByVNDBReleaseBackfill:
			vndbRel = append(vndbRel, it)
		case matchedByEGDMM:
			egDMM = append(egDMM, it)
		case matchedByEGSteam:
			egSteam = append(egSteam, it)
		case matchedByHLTBSteam:
			hltbSteam = append(hltbSteam, it)
		default:
			out[it.Hash] = unproven("unknown_chain_family", "matched_by "+it.MatchedBy+" is not a chain family")
		}
	}
	if err := verifyVNDBReleaseChain(db, vndbRel, out); err != nil {
		for _, it := range vndbRel {
			out[it.Hash] = chainResult{Reason: err.Error()}
		}
	}
	if err := verifyEGStoreChain(db, up.EG, reg, egDMM, "dmm", out); err != nil {
		for _, it := range egDMM {
			out[it.Hash] = chainResult{Reason: err.Error()}
		}
	}
	if err := verifyEGStoreChain(db, up.EG, reg, egSteam, "steam", out); err != nil {
		for _, it := range egSteam {
			out[it.Hash] = chainResult{Reason: err.Error()}
		}
	}
	if err := verifyHLTBSteamChain(db, up, reg, hltbSteam, out); err != nil {
		for _, it := range hltbSteam {
			out[it.Hash] = chainResult{Reason: err.Error()}
		}
	}
	return out, nil
}

func unproven(step, detail string) chainResult {
	return chainResult{
		Verdict: VerdictChainUnproven, Confidence: 0,
		Reason:   step + ": " + detail,
		Evidence: map[string]any{"steps": []chainStep{{Name: step, OK: false, Detail: detail}}},
	}
}

func verified(steps []chainStep) chainResult {
	return chainResult{
		Verdict: VerdictChainVerified, Confidence: 1, Reason: "chain verified",
		Evidence: map[string]any{"steps": steps},
	}
}

func verifyVNDBReleaseChain(db *gorm.DB, items []refItem, out map[string]chainResult) error {
	if len(items) == 0 {
		return nil
	}
	relIDs := make([]int64, 0, len(items))
	rids := make([]string, 0, len(items))
	seenRel, seenRID := map[int64]struct{}{}, map[string]struct{}{}
	for _, it := range items {
		if _, ok := seenRel[it.EntityID]; !ok {
			seenRel[it.EntityID] = struct{}{}
			relIDs = append(relIDs, it.EntityID)
		}
		if _, ok := seenRID[it.ExternalID]; !ok {
			seenRID[it.ExternalID] = struct{}{}
			rids = append(rids, it.ExternalID)
		}
	}
	type relRow struct {
		ID        int64   `gorm:"column:id"`
		WorkID    int64   `gorm:"column:work_id"`
		Title     *string `gorm:"column:title"`
		ReleasedY *int16  `gorm:"column:released_y"`
		ReleasedM *int16  `gorm:"column:released_m"`
		ReleasedD *int16  `gorm:"column:released_d"`
	}
	rels := map[int64]relRow{}
	for _, chunk := range chunkBy(relIDs, 500) {
		var rows []relRow
		if err := db.Raw(`SELECT id, work_id, title, released_y, released_m, released_d
			FROM catalog_release WHERE id IN ?`, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			rels[r.ID] = r
		}
	}
	workIDs := make([]int64, 0)
	seenW := map[int64]struct{}{}
	for _, r := range rels {
		if _, ok := seenW[r.WorkID]; ok {
			continue
		}
		seenW[r.WorkID] = struct{}{}
		workIDs = append(workIDs, r.WorkID)
	}
	workVNDB := map[int64][]string{}
	for _, chunk := range chunkBy(workIDs, 500) {
		var rows []struct {
			WorkID int64  `gorm:"column:entity_id"`
			VID    string `gorm:"column:external_id"`
		}
		if err := db.Raw(`SELECT entity_id, external_id FROM catalog_external_ref
			WHERE entity_type = ? AND source_id = (SELECT id FROM catalog_source WHERE key = 'vndb')
			  AND link_kind = ? AND dead_at IS NULL AND entity_id IN ?`,
			model.EntityTypeWork, model.LinkKindExact, chunk).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			workVNDB[r.WorkID] = append(workVNDB[r.WorkID], r.VID)
		}
	}
	rvn := map[string][]string{}
	relDate := map[string]int{}
	relTitles := map[string][]string{}
	for _, chunk := range chunkBy(rids, 500) {
		var vnRows []struct {
			ID  string `gorm:"column:id"`
			VID string `gorm:"column:vid"`
		}
		if err := db.Raw(`SELECT id, vid FROM src_vndb.releases_vn WHERE id IN ?`, chunk).Scan(&vnRows).Error; err != nil {
			return err
		}
		for _, r := range vnRows {
			rvn[r.ID] = append(rvn[r.ID], r.VID)
		}
		var dates []struct {
			ID       string `gorm:"column:id"`
			Released int    `gorm:"column:released"`
		}
		if err := db.Raw(`SELECT id, released FROM src_vndb.releases WHERE id IN ?`, chunk).Scan(&dates).Error; err != nil {
			return err
		}
		for _, r := range dates {
			relDate[r.ID] = r.Released
		}
		var titles []struct {
			ID    string `gorm:"column:id"`
			Title string `gorm:"column:title"`
			Latin string `gorm:"column:latin"`
		}
		if err := db.Raw(`SELECT id, title, latin FROM src_vndb.releases_titles WHERE id IN ?`, chunk).Scan(&titles).Error; err != nil {
			return err
		}
		for _, r := range titles {
			if strings.TrimSpace(r.Title) != "" {
				relTitles[r.ID] = append(relTitles[r.ID], r.Title)
			}
			if strings.TrimSpace(r.Latin) != "" {
				relTitles[r.ID] = append(relTitles[r.ID], r.Latin)
			}
		}
	}
	for _, it := range items {
		rel, ok := rels[it.EntityID]
		if !ok {
			out[it.Hash] = unproven("release_belongs_to_work", "catalog_release "+strconv.FormatInt(it.EntityID, 10)+" not found")
			continue
		}
		anchors := workVNDB[rel.WorkID]
		vids := rvn[it.ExternalID]
		linked := false
		for _, vid := range vids {
			for _, a := range anchors {
				if vid == a {
					linked = true
					break
				}
			}
		}
		steps := []chainStep{
			{Name: "release_belongs_to_work", OK: true, Detail: fmt.Sprintf("work %d", rel.WorkID)},
			{Name: "releases_vn_to_exact_work_vndb", OK: linked, Detail: fmt.Sprintf("rid=%s vids=%v work_exact=%v", it.ExternalID, vids, anchors)},
		}
		if !linked {
			out[it.Hash] = unproven(steps[1].Name, steps[1].Detail)
			continue
		}
		dateOK := vndbDateEquals(rel.ReleasedY, rel.ReleasedM, rel.ReleasedD, relDate[it.ExternalID])
		titleOK := titleEqualsAny(derefStr(rel.Title), relTitles[it.ExternalID])
		corroborated := dateOK || titleOK
		steps = append(steps, chainStep{
			Name: "date_or_title_corroboration", OK: corroborated,
			Detail: fmt.Sprintf("date_ok=%v title_ok=%v catalog_title=%q vndb_released=%d", dateOK, titleOK, derefStr(rel.Title), relDate[it.ExternalID]),
		})
		if !corroborated {
			out[it.Hash] = unproven("date_or_title_corroboration", steps[len(steps)-1].Detail)
			continue
		}
		out[it.Hash] = verified(steps)
	}
	return nil
}

func vndbDateEquals(catY, catM, catD *int16, released int) bool {
	if catY == nil || released <= 0 {
		return false
	}
	vy := released / 10000
	vm := (released / 100) % 100
	vd := released % 100
	if int(*catY) != vy {
		return false
	}
	catHasM := catM != nil
	vndbHasM := vm >= 1 && vm <= 12
	if catHasM != vndbHasM {
		return false
	}
	if catHasM && int(*catM) != vm {
		return false
	}
	catHasD := catD != nil
	vndbHasD := vd >= 1 && vd <= 31
	if catHasD != vndbHasD {
		return false
	}
	if catHasD && int(*catD) != vd {
		return false
	}
	return true
}

func titleEqualsAny(catalog string, titles []string) bool {
	catalog = strings.TrimSpace(catalog)
	if catalog == "" {
		return false
	}
	for _, t := range titles {
		if strings.EqualFold(catalog, strings.TrimSpace(t)) {
			return true
		}
	}
	return false
}

func verifyEGStoreChain(db, eg *gorm.DB, reg sourceReg, items []refItem, store string, out map[string]chainResult) error {
	if len(items) == 0 {
		return nil
	}
	if eg == nil {
		for _, it := range items {
			out[it.Hash] = unproven("eg_unavailable", "erogamescape connection is nil")
		}
		return nil
	}
	workIDs := make([]int64, 0, len(items))
	seen := map[int64]struct{}{}
	for _, it := range items {
		if _, ok := seen[it.EntityID]; ok {
			continue
		}
		seen[it.EntityID] = struct{}{}
		workIDs = append(workIDs, it.EntityID)
	}
	vndbSrc, egSrc := reg.id(sourceKeyVNDB), reg.id(sourceKeyEG)
	workVNDB := map[int64][]string{}
	workEG := map[int64][]int64{}
	for _, chunk := range chunkBy(workIDs, 500) {
		var rows []struct {
			WorkID     int64  `gorm:"column:entity_id"`
			SourceID   int16  `gorm:"column:source_id"`
			ExternalID string `gorm:"column:external_id"`
		}
		if err := db.Raw(`SELECT entity_id, source_id, external_id FROM catalog_external_ref
			WHERE entity_type = ? AND link_kind = ? AND dead_at IS NULL AND entity_id IN ? AND source_id IN ?`,
			model.EntityTypeWork, model.LinkKindExact, chunk, []int16{vndbSrc, egSrc}).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			switch r.SourceID {
			case vndbSrc:
				workVNDB[r.WorkID] = append(workVNDB[r.WorkID], r.ExternalID)
			case egSrc:
				if n, err := strconv.ParseInt(r.ExternalID, 10, 64); err == nil {
					workEG[r.WorkID] = append(workEG[r.WorkID], n)
				}
			}
		}
	}
	egIDs := make([]int64, 0)
	seenEG := map[int64]struct{}{}
	for _, ids := range workEG {
		for _, id := range ids {
			if _, ok := seenEG[id]; ok {
				continue
			}
			seenEG[id] = struct{}{}
			egIDs = append(egIDs, id)
		}
	}
	type egGame struct {
		ID    int64  `gorm:"column:id"`
		VNDB  string `gorm:"column:vndb"`
		Steam *int64 `gorm:"column:steam"`
		DMM   string `gorm:"column:dmm"`
	}
	games := map[int64]egGame{}
	for _, chunk := range chunkBy(egIDs, 500) {
		if len(chunk) == 0 {
			continue
		}
		var rows []egGame
		if err := eg.Raw(`SELECT id, vndb::text AS vndb, steam, coalesce(dmm, '') AS dmm FROM games WHERE id IN ?`, chunk).
			Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			games[r.ID] = r
		}
	}
	for _, it := range items {
		vs := workVNDB[it.EntityID]
		es := workEG[it.EntityID]
		if len(es) == 0 {
			out[it.Hash] = unproven("work_exact_eg", "work holds no exact erogamescape anchor")
			continue
		}
		ok := false
		var detail string
		for _, e := range es {
			g, found := games[e]
			if !found {
				detail = fmt.Sprintf("eg game %d missing from staging", e)
				continue
			}
			vndbConflict := vndbDisagrees(vs, g.VNDB)
			storeHit := false
			switch store {
			case "dmm":
				storeHit = g.DMM != "" && g.DMM == it.ExternalID
			case "steam":
				storeHit = g.Steam != nil && strconv.FormatInt(*g.Steam, 10) == it.ExternalID
			}
			detail = fmt.Sprintf("eg=%d vndb=%s store_%s=%s catalog_ext=%s vndb_conflict=%v store_hit=%v",
				e, g.VNDB, store, egStoreID(g.Steam, g.DMM, store), it.ExternalID, vndbConflict, storeHit)
			if storeHit && !vndbConflict {
				ok = true
				break
			}
		}
		steps := []chainStep{
			{Name: "work_exact_eg", OK: true, Detail: fmt.Sprintf("%v", es)},
			{Name: "eg_row_store_id", OK: ok, Detail: detail},
		}
		if !ok {
			out[it.Hash] = unproven("eg_row_store_id", detail)
			continue
		}
		out[it.Hash] = verified(steps)
	}
	return nil
}

// vndbDisagrees reports the one case where the vndb leg carries information:
// the work and the staged EG row both name a vndb id and they differ, which
// says the exact erogamescape anchor is pointing at the wrong game.
//
// It used to be a precondition — a work with no vndb anchor could not prove any
// chain — and on 2026-09-14 that left 2,921 dmm/steam refs stuck at
// chain-unproven with the reason "work holds no exact vndb anchor", every one
// of which the staged EG row confirmed once asked. An empty column on either
// side is an absence, not a disagreement. Measured over the 16,030 works that
// hold both anchors, EG's vndb column agrees with ours 16,010 times, so the
// anchor alone carries the association and this only has to catch the other 20.
func vndbDisagrees(workVNDB []string, egVNDB string) bool {
	g := normVNDBID(egVNDB)
	if g == "" || len(workVNDB) == 0 {
		return false
	}
	for _, v := range workVNDB {
		if n := normVNDBID(v); n == "" || n == g {
			return false
		}
	}
	return true
}

func egStoreID(steam *int64, dmm, store string) string {
	if store == "steam" && steam != nil {
		return strconv.FormatInt(*steam, 10)
	}
	return dmm
}

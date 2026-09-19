package getchuattach

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/titlekey"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func planMints(snap snapshot, leftover []item, st *Stats) []plannedAction {
	var mintable []item
	for _, it := range leftover {
		if mintGate(it, snap, st) {
			continue
		}
		mintable = append(mintable, it)
	}
	if len(mintable) == 0 {
		return nil
	}
	groups := groupMintItems(mintable)
	var out []plannedAction
	for _, grp := range groups {
		sort.Slice(grp, func(i, j int) bool { return getchuIDLess(grp[i].GetchuID, grp[j].GetchuID) })
		related, unmapped := groupRelations(grp, snap)
		if len(related) == 0 && unmapped {
			st.UnmappedRelations++
			continue
		}
		primary := choosePrimaryItem(grp)
		hits := related
		if len(hits) > maxCandidates {
			hits = hits[:maxCandidates]
		}
		p := plannedAction{
			Action: actionMint, GetchuID: primary.GetchuID, GetchuIDs: memberIDs(grp),
			MatchedBy: ruleWorkImport, Hits: hits, Members: grp, Primary: primary,
			Quarantine: len(related) > 0,
		}
		st.MintGroups++
		if p.Quarantine {
			st.MintedQuarantined++
			st.Candidates += len(hits)
		} else {
			st.MintedLive++
		}
		out = append(out, p)
	}
	return out
}

func groupMintItems(items []item) [][]item {
	keys := make([][]string, len(items))
	for i, it := range items {
		keys[i] = []string{titlekey.Loose(BaseTitle(it.Title))}
	}
	idxGroups := groupByKeyLists(keys, func(i, j int) bool { return foldableItems(items[i], items[j]) })
	out := make([][]item, len(idxGroups))
	for i, idxs := range idxGroups {
		grp := make([]item, len(idxs))
		for j, ix := range idxs {
			grp[j] = items[ix]
		}
		out[i] = grp
	}
	return out
}

func choosePrimaryItem(grp []item) item {
	best := grp[0]
	for _, it := range grp[1:] {
		if itemEarlier(it, best) {
			best = it
		}
	}
	return best
}

func itemEarlier(a, b item) bool {
	da, errA := parseGetchuDate(a.ReleaseDate)
	db, errB := parseGetchuDate(b.ReleaseDate)
	switch {
	case errA == nil && errB == nil && !da.Equal(db):
		return da.Before(db)
	case errA == nil && errB != nil:
		return true
	case errA != nil && errB == nil:
		return false
	}
	return getchuIDLess(a.GetchuID, b.GetchuID)
}

func getchuIDLess(a, b string) bool {
	na, errA := strconv.ParseInt(a, 10, 64)
	nb, errB := strconv.ParseInt(b, 10, 64)
	if errA == nil && errB == nil {
		return na < nb
	}
	return a < b
}

func memberIDs(grp []item) []string {
	ids := make([]string, len(grp))
	for i, it := range grp {
		ids[i] = it.GetchuID
	}
	sort.Strings(ids)
	return ids
}

// groupRelations returns the works a group resembles, strongest first: a hit
// on the whole title before a hit on a shorter whitespace prefix, then by id.
// The quarantine files candidates for the first maxCandidates only, and a
// series prefix such as ランス reaches every earlier work of the series.
func groupRelations(grp []item, snap snapshot) ([]int64, bool) {
	rank := map[int64]int{}
	note := func(w int64, r int) {
		if cur, ok := rank[w]; !ok || r < cur {
			rank[w] = r
		}
	}
	unmapped := false
	for _, it := range grp {
		for lvl, pfx := range titlePrefixes(it.Title) {
			for _, k := range titlekey.Keys(pfx) {
				for _, w := range snap.relTitleIndex[k] {
					note(w, lvl)
				}
			}
			loose := titlekey.Loose(pfx)
			if loose == "" {
				continue
			}
			for _, w := range snap.relLooseIndex[loose] {
				note(w, lvl)
			}
			if utf8.RuneCountInString(loose) < 2 {
				continue
			}
			for _, g := range snap.egByLoose[loose] {
				ws := uniqueIDs(snap.egWorks[g.ID])
				if len(ws) == 0 {
					unmapped = true
				}
				for _, w := range ws {
					note(w, lvl)
				}
			}
		}
	}
	out := make([]int64, 0, len(rank))
	for w := range rank {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool {
		if rank[out[i]] != rank[out[j]] {
			return rank[out[i]] < rank[out[j]]
		}
		return out[i] < out[j]
	})
	return out, unmapped && len(out) == 0
}

func applyMints(ctx context.Context, db *gorm.DB, ids registryIDs, snap snapshot, mints []plannedAction) (touched []int64, written, errors int) {
	for start := 0; start < len(mints); start += writeChunk {
		end := start + writeChunk
		if end > len(mints) {
			end = len(mints)
		}
		chunk := mints[start:end]
		var chunkTouched []int64
		var chunkWritten int
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			hosts, n, err := createMintChunk(tx, ids, snap, chunk)
			if err != nil {
				return err
			}
			chunkWritten = n
			chunkTouched = hosts
			return nil
		})
		if err != nil {
			errors++
			slog.Warn("getchuattach mint chunk", "start", start, "err", err)
			continue
		}
		written += chunkWritten
		touched = append(touched, chunkTouched...)
	}
	return touched, written, errors
}

func createMintChunk(tx *gorm.DB, ids registryIDs, snap snapshot, chunk []plannedAction) ([]int64, int, error) {
	works := make([]model.CatalogWork, len(chunk))
	for i, p := range chunk {
		status := model.WorkStatusLive
		if p.Quarantine {
			status = model.WorkStatusQuarantine
		}
		works[i] = model.CatalogWork{
			MediumID: ids.galgame, OLang: model.OLangDefault,
			DisplayName: BaseTitle(p.Primary.Title), ContentRating: model.ContentRatingR18,
			Status: status, Extra: emptyJSON(), FieldProvenance: emptyJSON(),
		}
	}
	if err := tx.CreateInBatches(works, 1000).Error; err != nil {
		return nil, 0, err
	}

	var titles []model.CatalogWorkTitle
	var releases []model.CatalogRelease
	memberOf := make([][]int, len(chunk))
	for i, p := range chunk {
		wid := works[i].ID
		titles = append(titles, model.CatalogWorkTitle{
			WorkID: wid, Lang: "ja", Title: works[i].DisplayName, Kind: model.WorkTitleKindOfficial,
		})
		start := len(releases)
		for _, m := range p.Members {
			y, mo, d := releaseParts(m.ReleaseDate, snap.now)
			title := m.Title
			releases = append(releases, model.CatalogRelease{
				WorkID: wid, Kind: releaseKind(m.Media), Title: strCopy(title),
				ReleasedY: y, ReleasedM: mo, ReleasedD: d,
				Extra: emptyJSON(), FieldProvenance: emptyJSON(),
			})
		}
		memberOf[i] = []int{start, len(releases)}
	}
	if err := tx.CreateInBatches(titles, 1000).Error; err != nil {
		return nil, 0, err
	}
	if err := tx.CreateInBatches(releases, 1000).Error; err != nil {
		return nil, 0, err
	}

	var refs []model.CatalogExternalRef
	var revs []model.CatalogRevision
	var edges []model.CatalogWorkLabel
	var cands []model.CatalogMatchCandidate
	src := ids.getchu
	written := 0
	hosts := make([]int64, 0, len(chunk))
	titlesByWork := map[int64][]model.CatalogWorkTitle{}
	for _, t := range titles {
		titlesByWork[t.WorkID] = append(titlesByWork[t.WorkID], t)
	}
	for i, p := range chunk {
		wid := works[i].ID
		hosts = append(hosts, wid)
		revs = append(revs, importedRev(model.EntityTypeWork, wid, workSnapshotJSON(works[i], titlesByWork[wid])))
		span := memberOf[i]
		for j, m := range p.Members {
			rel := releases[span[0]+j]
			refs = append(refs, model.CatalogExternalRef{
				EntityType: model.EntityTypeRelease, EntityID: rel.ID,
				SourceID: ids.getchu, ExternalID: m.GetchuID,
				LinkKind: model.LinkKindExact, MatchedBy: ruleWorkImport,
			})
			revs = append(revs, importedRev(model.EntityTypeRelease, rel.ID, releaseSnapshotJSON(rel)))
			written++
		}
		if bid, ok := uniqueEGBrand(p.Primary, snap); ok {
			if lid, ok := snap.brandLabel[bid]; ok {
				edges = append(edges, model.CatalogWorkLabel{
					WorkID: wid, LabelID: lid, Kind: model.WorkLabelKindDeveloper, SourceID: &src,
				})
			}
		}
		if p.Quarantine {
			for _, hit := range p.Hits {
				a, b := wid, hit
				if b < a {
					a, b = b, a
				}
				cands = append(cands, model.CatalogMatchCandidate{
					EntityType: model.EntityTypeWork, AID: a, BID: b,
					Reason: model.CandidateReasonNameNormEqual,
					Status: model.CandidateStatusPending,
				})
			}
		}
	}
	if err := tx.CreateInBatches(refs, 1000).Error; err != nil {
		return nil, 0, err
	}
	if err := tx.CreateInBatches(revs, 1000).Error; err != nil {
		return nil, 0, err
	}
	if len(edges) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&edges).Error; err != nil {
			return nil, 0, err
		}
	}
	if len(cands) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&cands).Error; err != nil {
			return nil, 0, err
		}
	}
	return hosts, written, nil
}

func releaseKind(media string) int16 {
	n := nfkcFold(media)
	if strings.Contains(n, "ダウンロード") || strings.HasPrefix(strings.TrimSpace(media), "DL") {
		return model.ReleaseKindDigital
	}
	return model.ReleaseKindPhysical
}

func strCopy(s string) *string {
	v := s
	return &v
}

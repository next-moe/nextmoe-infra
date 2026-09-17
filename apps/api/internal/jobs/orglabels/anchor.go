package orglabels

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type orgRec struct {
	extID        string
	works        []int64
	attribWorks  []int64
	editionAware bool
	nameNorms    []string
	displayName  string
	latin        *string
	lang         string
	newKind      int16
	canCreate    bool
}

type resKind int

const (
	resAnchorExisting resKind = iota
	resNewLabel
	resSkipNoMatch
	resSkipAmbiguous
	resSkipUngradeable
)

type gradeResult struct {
	kind    resKind
	labelID int64
	tier    int16
	rule    string
	share   int
}

type grader struct {
	workLabels map[int64][]int64
	labelNorms map[string][]int64
	labelLoose map[string][]int64
	rules      ruleSet
}

func (g *grader) grade(o *orgRec) gradeResult {
	share := make(map[int64]int)
	for _, w := range o.works {
		for _, l := range g.workLabels[w] {
			share[l]++
		}
	}
	nameHit := make(map[int64]bool)
	for _, n := range o.nameNorms {
		if n == "" {
			continue
		}
		for _, l := range g.labelNorms[n] {
			nameHit[l] = true
		}
	}

	bestLabel := int64(0)
	bestShare := 0
	bestName := false
	for l, s := range share {
		eligible := s >= 2 || (s == 1 && nameHit[l])
		if !eligible {
			continue
		}
		if better(s, nameHit[l], l, bestShare, bestName, bestLabel) {
			bestLabel, bestShare, bestName = l, s, nameHit[l]
		}
	}
	if bestLabel != 0 {
		rule := g.rules.coworks
		if bestShare == 1 {
			rule = g.rules.coworkName
		}
		return gradeResult{kind: resAnchorExisting, labelID: bestLabel, tier: 0, rule: rule, share: bestShare}
	}

	var nameOnly []int64
	for l := range nameHit {
		if share[l] == 0 {
			nameOnly = append(nameOnly, l)
		}
	}
	if len(nameOnly) == 1 {
		return gradeResult{kind: resAnchorExisting, labelID: nameOnly[0], tier: 1, rule: g.rules.nameOnly, share: 0}
	}
	if len(nameOnly) > 1 {
		return gradeResult{kind: resSkipAmbiguous}
	}

	if len(share) > 0 {
		return gradeResult{kind: resSkipUngradeable}
	}
	if !o.canCreate || len(o.works) == 0 {
		return gradeResult{kind: resSkipNoMatch}
	}
	// The 2026-09-17 dry run planned four labels whose names differ from a
	// live label only by 株式会社, (株) or punctuation; the name index is an
	// NFKC equality and let every one of them through as new.
	switch twins := looseHits(o, g.labelLoose); len(twins) {
	case 0:
		return gradeResult{kind: resNewLabel}
	case 1:
		return gradeResult{kind: resAnchorExisting, labelID: twins[0], tier: model.LinkKindProbable, rule: g.rules.nameLoose}
	default:
		return gradeResult{kind: resSkipAmbiguous}
	}
}

func better(s int, name bool, id int64, bestS int, bestName bool, bestID int64) bool {
	if s != bestS {
		return s > bestS
	}
	if name != bestName {
		return name
	}
	return bestID == 0 || id < bestID
}

type AnchorStats struct {
	Orgs            int
	Already         int
	AnchorsExact    int
	AnchorsProbable int
	NewLabels       int
	NewEdges        int
	Conflict        int
	SkipNoMatch     int
	SkipAmbiguous   int
	SkipUngradeable int
	SkipRejected    int
	SkipDeferred    int
	VNDBInAnchored  int
	Errors          int
	Spine           SpineStats
}

func (s *AnchorStats) add(o AnchorStats) {
	s.Orgs += o.Orgs
	s.Already += o.Already
	s.AnchorsExact += o.AnchorsExact
	s.AnchorsProbable += o.AnchorsProbable
	s.NewLabels += o.NewLabels
	s.NewEdges += o.NewEdges
	s.Conflict += o.Conflict
	s.SkipNoMatch += o.SkipNoMatch
	s.SkipAmbiguous += o.SkipAmbiguous
	s.SkipUngradeable += o.SkipUngradeable
	s.SkipRejected += o.SkipRejected
	s.SkipDeferred += o.SkipDeferred
	s.VNDBInAnchored += o.VNDBInAnchored
	s.Errors += o.Errors
}

func RunAnchor(ctx context.Context, opts Opts) (AnchorStats, error) {
	needEG := opts.Source == "eg" || opts.Source == "all"
	catalog, eg, err := openPools(opts, needEG)
	if err != nil {
		return AnchorStats{}, err
	}
	return anchorAll(ctx, catalog, eg, opts.Source, opts.Limit, opts.Apply)
}

const maxAnchorPasses = 6

func loadGrader(db *gorm.DB) (*grader, error) {
	workLabels, err := loadLabelWorks(db)
	if err != nil {
		return nil, fmt.Errorf("load label works: %w", err)
	}
	labelNorms, err := loadLabelNorms(db)
	if err != nil {
		return nil, fmt.Errorf("load label norms: %w", err)
	}
	return &grader{workLabels: workLabels, labelNorms: labelNorms, labelLoose: looseIndex(labelNorms)}, nil
}

func anchorAll(ctx context.Context, catalog, eg *gorm.DB, source string, limit int, apply bool) (AnchorStats, error) {
	var total AnchorStats
	planned := newMintPlan()
	for pass := 1; pass <= maxAnchorPasses; pass++ {
		g, err := loadGrader(catalog)
		if err != nil {
			return total, err
		}
		var pt AnchorStats
		for _, src := range wantedSources(source) {
			orgs, rules, srcID, err := loadSource(catalog, eg, src, limit)
			if err != nil {
				return total, fmt.Errorf("load %s orgs: %w", src, err)
			}
			g.rules = rules
			st, err := anchorSource(ctx, catalog, g, orgs, srcID, apply, planned)
			if err != nil {
				return total, fmt.Errorf("anchor %s: %w", src, err)
			}
			slog.Info("org-label anchor source done", "source", src, "pass", pass, "apply", apply,
				"orgs", st.Orgs, "already", st.Already, "exact", st.AnchorsExact,
				"probable", st.AnchorsProbable, "new_labels", st.NewLabels, "new_edges", st.NewEdges,
				"conflict", st.Conflict, "skip_no_match", st.SkipNoMatch,
				"skip_ambiguous", st.SkipAmbiguous, "skip_ungradeable", st.SkipUngradeable,
				"skip_rejected", st.SkipRejected, "skip_deferred", st.SkipDeferred,
				"vndb_in_anchored", st.VNDBInAnchored)
			pt.add(st)
		}

		if slices.Contains(wantedSources(source), "vndb") {
			sp, err := runSpine(ctx, catalog, g.labelNorms, limit, apply, planned)
			if err != nil {
				return total, fmt.Errorf("spine pass: %w", err)
			}
			slog.Info("org-label spine pass done", "pass", pass, "apply", apply,
				"considered", sp.Considered, "minted", sp.Minted, "anchored", sp.Anchored,
				"candidates", sp.Candidates, "skip_claimed", sp.SkipClaimed,
				"skip_edgeless", sp.SkipEdgeless, "skip_alias_only", sp.SkipAliasOnly,
				"skip_loose_twin", sp.SkipLooseTwin, "skip_deferred", sp.SkipDeferred,
				"errors", sp.Errors)
			pt.Spine = sp
		}

		if pass == 1 {
			total = pt
		} else {
			total.AnchorsExact += pt.AnchorsExact
			total.AnchorsProbable += pt.AnchorsProbable
			total.NewLabels += pt.NewLabels
			total.NewEdges += pt.NewEdges
			total.VNDBInAnchored += pt.VNDBInAnchored
			total.Errors += pt.Errors
			total.SkipDeferred = pt.SkipDeferred
			total.Spine.addWrites(pt.Spine)
		}
		total.Spine.setState(pt.Spine)
		if !apply || pt.AnchorsExact+pt.AnchorsProbable+pt.NewLabels+pt.Spine.writes() == 0 {
			break
		}
	}
	return total, nil
}

func wantedSources(sel string) []string {
	if sel == "all" {
		return []string{"vndb", "bangumi", "eg"}
	}
	return []string{sel}
}

func anchorSource(ctx context.Context, db *gorm.DB, g *grader, orgs []orgRec, source int16, apply bool, planned *mintPlan) (AnchorStats, error) {
	ea, err := loadExistingAnchors(db, source)
	if err != nil {
		return AnchorStats{}, fmt.Errorf("load existing anchors: %w", err)
	}
	rejected, err := loadRejections(db, source)
	if err != nil {
		return AnchorStats{}, fmt.Errorf("load rejections: %w", err)
	}
	st := AnchorStats{Orgs: len(orgs)}

	type planItem struct {
		org *orgRec
		res gradeResult
	}
	var anchors, newLabels []planItem
	for i := range orgs {
		o := &orgs[i]
		if _, done := ea.byExtID[o.extID]; done {
			st.Already++
			continue
		}
		res := g.grade(o)
		switch res.kind {
		case resAnchorExisting:
			if _, no := rejected[rejKey(res.labelID, o.extID)]; no {
				st.SkipRejected++
				continue
			}
			anchors = append(anchors, planItem{o, res})
		case resNewLabel:
			newLabels = append(newLabels, planItem{o, res})
		case resSkipAmbiguous:
			st.SkipAmbiguous++
		case resSkipUngradeable:
			st.SkipUngradeable++
		default:
			st.SkipNoMatch++
		}
	}

	sort.Slice(anchors, func(i, j int) bool {
		a, b := anchors[i].res, anchors[j].res
		if a.share != b.share {
			return a.share > b.share
		}
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		return anchors[i].org.extID < anchors[j].org.extID
	})
	claimed := make(map[int64]bool, len(ea.claimedByLabel))
	for l := range ea.claimedByLabel {
		claimed[l] = true
	}

	refs := make([]model.CatalogExternalRef, 0, len(anchors))
	for _, it := range anchors {
		if claimed[it.res.labelID] {
			st.Conflict++
			slog.Debug("org-label anchor conflict — label already claimed by this source",
				"source", source, "ext_id", it.org.extID, "label_id", it.res.labelID)
			continue
		}
		claimed[it.res.labelID] = true
		refs = append(refs, model.CatalogExternalRef{
			EntityType: model.EntityTypeLabel, EntityID: it.res.labelID, SourceID: source,
			ExternalID: it.org.extID, LinkKind: it.res.tier, MatchedBy: it.res.rule,
		})
		if it.res.tier == 0 {
			st.AnchorsExact++
		} else {
			st.AnchorsProbable++
		}
		if source == sourceVNDB && !it.org.canCreate {
			st.VNDBInAnchored++
		}
	}
	if apply && len(refs) > 0 {
		res := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(refs, 1000)
		if res.Error != nil {
			return st, fmt.Errorf("batch insert anchors: %w", res.Error)
		}
	}

	for _, it := range newLabels {
		if planned.taken(source, it.org) {
			st.SkipDeferred++
			continue
		}
		planned.add(source, it.org)
		slog.Info("org-label new", "source", source, "ext_id", it.org.extID,
			"name", it.org.displayName, "works", len(it.org.works), "apply", apply)
		edges := len(it.org.works)
		if apply {
			n, err := mintLabel(ctx, db, source, it.org)
			if err != nil {
				st.Errors++
				slog.Warn("org-label mint failed", "source", source, "ext_id", it.org.extID, "error", err)
				continue
			}
			edges = n
		}
		st.NewLabels++
		st.NewEdges += edges
	}
	return st, nil
}

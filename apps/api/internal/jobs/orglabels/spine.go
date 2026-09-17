package orglabels

import (
	"context"
	"log/slog"
	"slices"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ruleSpineNew  = "rule:vndb-org-graph-new"
	ruleSpineName = "rule:vndb-org-graph-name"
)

type graphFacts struct {
	member   map[string]bool
	linkable map[string]bool
	parent   map[string]bool
}

type spineAct int

const (
	spineMint spineAct = iota
	spineAnchor
	spineCandidate
	spineSkipClaimed
	spineSkipAliasOnly
	spineSkipLooseTwin
	spineSkipDeferred
)

func matchLabels(norms []string, index map[string][]int64) []int64 {
	hit := make(map[int64]bool)
	for _, n := range norms {
		if n == "" {
			continue
		}
		for _, l := range index[n] {
			hit[l] = true
		}
	}
	out := make([]int64, 0, len(hit))
	for l := range hit {
		out = append(out, l)
	}
	slices.Sort(out)
	return out
}

type spinePlan struct {
	act     spineAct
	org     *orgRec
	labelID int64
	labels  []int64
}

type SpineStats struct {
	Considered    int
	Minted        int
	Anchored      int
	Candidates    int
	SkipClaimed   int
	SkipEdgeless  int
	SkipAliasOnly int
	SkipLooseTwin int
	SkipDeferred  int
	Errors        int
}

func (s *SpineStats) addWrites(o SpineStats) {
	s.Minted += o.Minted
	s.Anchored += o.Anchored
	s.Errors += o.Errors
}

func (s *SpineStats) setState(o SpineStats) {
	s.Considered = o.Considered
	s.Candidates = o.Candidates
	s.SkipClaimed = o.SkipClaimed
	s.SkipEdgeless = o.SkipEdgeless
	s.SkipAliasOnly = o.SkipAliasOnly
	s.SkipLooseTwin = o.SkipLooseTwin
	s.SkipDeferred = o.SkipDeferred
}

func (s SpineStats) writes() int { return s.Minted + s.Anchored }

func planSpine(orgs []orgRec, g graphFacts, ea *existingAnchors, labelNorms, displayNorms, loose map[string][]int64, planned *mintPlan) ([]spinePlan, SpineStats) {
	var st SpineStats
	claimed := make(map[int64]bool, len(ea.claimedByLabel))
	for l := range ea.claimedByLabel {
		claimed[l] = true
	}

	out := make([]spinePlan, 0, 64)
	for i := range orgs {
		o := &orgs[i]
		if !o.canCreate || !g.member[o.extID] || o.displayName == "" {
			continue
		}
		if _, done := ea.byExtID[o.extID]; done {
			continue
		}
		if !g.linkable[o.extID] {
			st.SkipEdgeless++
			continue
		}
		st.Considered++

		any := matchLabels(o.nameNorms, labelNorms)
		target := any
		if len(any) > 1 {
			target = matchLabels(o.nameNorms, displayNorms)
		}

		switch {
		case len(any) == 0 && len(looseHits(o, loose)) > 0:
			out = append(out, spinePlan{act: spineSkipLooseTwin, org: o, labels: looseHits(o, loose)})
			st.SkipLooseTwin++
		case len(any) == 0 && planned.taken(sourceVNDB, o):
			out = append(out, spinePlan{act: spineSkipDeferred, org: o})
			st.SkipDeferred++
		case len(any) == 0:
			planned.add(sourceVNDB, o)
			out = append(out, spinePlan{act: spineMint, org: o})
			st.Minted++
		case len(target) == 0:
			out = append(out, spinePlan{act: spineSkipAliasOnly, org: o, labels: any})
			st.SkipAliasOnly++
		case len(target) == 1 && !claimed[target[0]]:
			claimed[target[0]] = true
			out = append(out, spinePlan{act: spineAnchor, org: o, labelID: target[0]})
			st.Anchored++
		case len(target) == 1:
			out = append(out, spinePlan{act: spineSkipClaimed, org: o, labelID: target[0]})
			st.SkipClaimed++
		default:
			out = append(out, spinePlan{act: spineCandidate, org: o, labels: target})
			st.Candidates++
		}
	}
	return out, st
}

func spineKind(o *orgRec, g graphFacts) int16 {
	if g.parent[o.extID] && o.newKind == model.LabelKindGameBrand {
		return model.LabelKindPublisher
	}
	return o.newKind
}

func runSpine(ctx context.Context, db *gorm.DB, labelNorms map[string][]int64, limit int, apply bool, planned *mintPlan) (SpineStats, error) {
	orgs, _, _, err := loadSource(db, nil, "vndb", limit)
	if err != nil {
		return SpineStats{}, err
	}
	g, err := loadGraphFacts(db)
	if err != nil {
		return SpineStats{}, err
	}
	ea, err := loadExistingAnchors(db, sourceVNDB)
	if err != nil {
		return SpineStats{}, err
	}
	displayNorms, err := loadLabelDisplayNorms(db)
	if err != nil {
		return SpineStats{}, err
	}

	plans, st := planSpine(orgs, g, ea, labelNorms, displayNorms, looseIndex(labelNorms), planned)
	// The label pairs a producer's name is shared by used to be filed as
	// match candidates; 38 sat there on 2026-09-17 with nothing scheduled to
	// read them. They are logged for whoever merges labels instead.
	for _, p := range plans {
		switch p.act {
		case spineMint:
			slog.Info("org-label spine mint", "ext_id", p.org.extID, "name", p.org.displayName, "apply", apply)
		case spineCandidate, spineSkipLooseTwin:
			slog.Info("org-label spine held", "ext_id", p.org.extID, "name", p.org.displayName,
				"labels", p.labels, "loose", p.act == spineSkipLooseTwin)
		}
	}
	if !apply {
		return st, nil
	}

	refs := make([]model.CatalogExternalRef, 0, st.Anchored)
	for _, p := range plans {
		if p.act == spineAnchor {
			refs = append(refs, model.CatalogExternalRef{
				EntityType: model.EntityTypeLabel, EntityID: p.labelID, SourceID: sourceVNDB,
				ExternalID: p.org.extID, LinkKind: model.LinkKindExact, MatchedBy: ruleSpineName,
			})
		}
	}
	if len(refs) > 0 {
		if err := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
			CreateInBatches(refs, 1000).Error; err != nil {
			return st, err
		}
	}
	for _, p := range plans {
		if p.act != spineMint {
			continue
		}
		if err := mintSpineLabel(ctx, db, p.org, spineKind(p.org, g)); err != nil {
			st.Errors++
			st.Minted--
		}
	}
	return st, nil
}

func mintSpineLabel(ctx context.Context, db *gorm.DB, o *orgRec, kind int16) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		label := model.CatalogLabel{
			DisplayName:     o.displayName,
			Latin:           o.latin,
			Lang:            o.lang,
			Kind:            kind,
			FieldProvenance: mintProvenance(sourceVNDB, o),
		}
		if err := tx.Create(&label).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.CatalogExternalRef{
			EntityType: model.EntityTypeLabel, EntityID: label.ID, SourceID: sourceVNDB,
			ExternalID: o.extID, LinkKind: model.LinkKindExact, MatchedBy: ruleSpineNew,
		}).Error; err != nil {
			return err
		}
		return tx.Create(&model.CatalogRevision{
			EntityType: model.EntityTypeLabel, EntityID: label.ID, Revision: 1,
			Action: model.RevisionActionImported, Snapshot: labelSnapshot(label), IsMinor: false,
		}).Error
	})
}

func loadGraphFacts(db *gorm.DB) (graphFacts, error) {
	g := graphFacts{member: map[string]bool{}, linkable: map[string]bool{}, parent: map[string]bool{}}
	var rows []struct {
		ID       string `gorm:"column:id"`
		PID      string `gorm:"column:pid"`
		Relation string `gorm:"column:relation"`
		AType    string `gorm:"column:a_type"`
		BType    string `gorm:"column:b_type"`
	}
	if err := db.Raw(`
		SELECT r.id, r.pid, r.relation, a.type AS a_type, b.type AS b_type
		FROM src_vndb.producers_relations r
		JOIN src_vndb.producers a ON a.id = r.id
		JOIN src_vndb.producers b ON b.id = r.pid`).Scan(&rows).Error; err != nil {
		return g, err
	}
	corporate := func(t string) bool { return t == "co" || t == "ng" }
	for _, r := range rows {
		g.member[r.ID], g.member[r.PID] = true, true
		if corporate(r.BType) {
			g.linkable[r.ID] = true
		}
		if corporate(r.AType) {
			g.linkable[r.PID] = true
		}
		if r.Relation == "sub" || r.Relation == "imp" {
			g.parent[r.ID] = true
		}
	}
	return g, nil
}

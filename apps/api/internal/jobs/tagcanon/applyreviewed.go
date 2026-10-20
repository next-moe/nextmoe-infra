package tagcanon

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const singleCoreUsageFloor = 1000

type ApplyReviewedOpts struct {
	DSN       string
	Decisions string
	Apply     bool
}

type ApplyReviewedStats struct {
	ApprovedPairs   int
	Groups          int
	SingleRows      int
	MapRows         int
	RejectRows      int
	TagsCreated     int
	TagsConflict    int
	MapsCreated     int
	MapsConflict    int
	RejectsCreated  int
	RejectsConflict int
	TierUpdated     int
	Errors          int
}

func ApplyReviewed(ctx context.Context, opts ApplyReviewedOpts) (*ApplyReviewedStats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn)")
	}
	if opts.Decisions == "" {
		return nil, fmt.Errorf("decisions path is required (--decisions)")
	}
	recs, err := readRecords(opts.Decisions)
	if err != nil {
		return nil, err
	}
	db, err := openGorm(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}
	src, err := resolveSources(ctx, db)
	if err != nil {
		return nil, err
	}
	keyToID := map[string]int16{sourceKeyVNDB: src.vndb, sourceKeyBangumi: src.bangumi,
		sourceKeyDlsite: src.dlsite, sourceKeyCurated: src.curated}

	st := &ApplyReviewedStats{}

	groups, absorbed := groupsFromPairs(recs, keyToID, st)
	singles := singlesFromRecords(recs, keyToID, absorbed)
	maps := mapsFromRecords(recs, keyToID, st)
	rejects := rejectsFromRecords(recs, keyToID, st)
	st.Groups = len(groups)
	st.SingleRows = len(singles)
	st.MapRows = len(maps)
	st.RejectRows = len(rejects)

	if !opts.Apply {
		slog.Info("apply-reviewed (dry)", "approved_pairs", st.ApprovedPairs,
			"groups", st.Groups, "single_rows", st.SingleRows,
			"map_rows", st.MapRows, "reject_rows", st.RejectRows, "errors", st.Errors)
		return st, nil
	}

	inner := &Stats{JunkByReason: map[string]int{}}
	w := &writer{db: db, stats: inner}

	for _, g := range groups {
		w.writeGroup(ctx, g, true)
	}
	for _, s := range singles {
		id, ok := w.ensureTag(ctx, s.group)
		if !ok {
			continue
		}
		writeMemberMap(ctx, db, s.group.Members[0], id, inner)
		if n := stampTierKind(ctx, db, id, s.group.Tier, s.group.Kind); n > 0 {
			st.TierUpdated += n
		}
	}
	for _, m := range maps {
		id := m.targetID
		if id == 0 {
			created, ok := w.ensureTag(ctx, m.create)
			if !ok {
				continue
			}
			id = created
		} else if !tagIDExists(ctx, db, id) {
			inner.Errors++
			slog.Warn("map target canonical does not exist", "source", m.member.SourceID, "name", m.member.Name, "target", id)
			continue
		}
		writeMemberMap(ctx, db, m.member, id, inner)
	}
	for _, r := range rejects {
		writeRejection(ctx, db, r, st, inner)
	}

	st.TagsCreated = inner.TagsCreated
	st.TagsConflict = inner.TagsConflict
	st.MapsCreated = inner.MapsCreated
	st.MapsConflict = inner.MapsConflict
	st.Errors += inner.Errors
	slog.Info("apply-reviewed done", "groups", st.Groups, "single_rows", st.SingleRows,
		"map_rows", st.MapRows, "reject_rows", st.RejectRows,
		"tags_created", st.TagsCreated, "tags_conflict", st.TagsConflict,
		"maps_created", st.MapsCreated, "maps_conflict", st.MapsConflict,
		"rejects_created", st.RejectsCreated, "rejects_conflict", st.RejectsConflict,
		"tier_updated", st.TierUpdated, "errors", st.Errors)
	return st, nil
}

type singleRow struct {
	group group
}

func groupsFromPairs(recs []pairRec, keyToID map[string]int16, st *ApplyReviewedStats) ([]group, map[string]struct{}) {
	uf := newDSU()
	info := map[string]vocabEntry{}
	for _, r := range recs {
		if r.Kind != "pair" || !r.Approve || Relation(r.Relation) != RelExact {
			continue
		}
		ia, oka := keyToID[r.ASource]
		ib, okb := keyToID[r.BSource]
		if !oka || !okb {
			continue
		}
		ka, kb := mapKey(ia, r.AName), mapKey(ib, r.BName)
		if _, seen := info[ka]; !seen {
			info[ka] = vocabEntry{SourceID: ia, Name: r.AName, Norm: normalize(r.AName), Usage: r.AUsage}
		}
		if _, seen := info[kb]; !seen {
			info[kb] = vocabEntry{SourceID: ib, Name: r.BName, Norm: normalize(r.BName), Usage: r.BUsage}
		}
		st.ApprovedPairs++
		uf.union(ka, kb)
	}

	comps := map[string][]vocabEntry{}
	for k := range info {
		root := uf.find(k)
		comps[root] = append(comps[root], info[k])
	}
	absorbed := map[string]struct{}{}
	var groups []group
	for _, members := range comps {
		srcs := map[int16]struct{}{}
		kind := model.TagKindContent
		for _, m := range members {
			srcs[m.SourceID] = struct{}{}
			if isMeta(m.Norm) {
				kind = model.TagKindMeta
			}
		}
		if len(srcs) < 2 {
			continue
		}
		for _, m := range members {
			absorbed[mapKey(m.SourceID, m.Name)] = struct{}{}
		}
		sort.Slice(members, func(i, j int) bool {
			if members[i].SourceID != members[j].SourceID {
				return members[i].SourceID < members[j].SourceID
			}
			return members[i].Name < members[j].Name
		})
		groups = append(groups, group{
			CanonicalName: pickCanonicalName(members),
			Tier:          model.TagTierCore,
			Kind:          kind,
			Members:       members,
			sourceCount:   len(srcs),
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].CanonicalName < groups[j].CanonicalName })
	return groups, absorbed
}

func singlesFromRecords(recs []pairRec, keyToID map[string]int16, absorbed map[string]struct{}) []singleRow {
	var out []singleRow
	for _, r := range recs {
		if r.Kind != "single" || !r.Approve {
			continue
		}
		id, ok := keyToID[r.Source]
		if !ok || r.Tier == nil || r.Kind_ == nil {
			continue
		}
		k := mapKey(id, r.Name)
		if _, hit := absorbed[k]; hit {
			continue
		}
		tier := *r.Tier
		if tier == model.TagTierCore && r.Usage < singleCoreUsageFloor {
			tier = model.TagTierLongtail
		}
		out = append(out, singleRow{
			group: group{
				CanonicalName: r.Name,
				Tier:          tier,
				Kind:          *r.Kind_,
				Sexual:        r.Sexual,
				Members:       []vocabEntry{{SourceID: id, Name: r.Name, Norm: normalize(r.Name), Usage: r.Usage}},
				sourceCount:   1,
			},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].group.CanonicalName < out[j].group.CanonicalName })
	return out
}

type mapRow struct {
	member   vocabEntry
	targetID int64
	create   group
}

func mapsFromRecords(recs []pairRec, keyToID map[string]int16, st *ApplyReviewedStats) []mapRow {
	var out []mapRow
	for _, r := range recs {
		if r.Kind != "map" || !r.Approve {
			continue
		}
		id, ok := keyToID[r.Source]
		if !ok || r.Name == "" || (r.MapToID == 0 && r.MapTo == "") {
			st.Errors++
			slog.Warn("malformed map record", "source", r.Source, "name", r.Name, "map_to_id", r.MapToID, "map_to", r.MapTo)
			continue
		}
		m := mapRow{member: vocabEntry{SourceID: id, Name: r.Name, Norm: normalize(r.Name), Usage: r.Usage}, targetID: r.MapToID}
		if m.targetID == 0 {
			tier, kind := model.TagTierLongtail, model.TagKindContent
			if r.Tier != nil {
				tier = *r.Tier
			}
			if r.Kind_ != nil {
				kind = *r.Kind_
			}
			m.create = group{CanonicalName: r.MapTo, Tier: tier, Kind: kind, Sexual: r.Sexual,
				Members: []vocabEntry{m.member}, sourceCount: 1}
		}
		out = append(out, m)
	}
	return out
}

type rejectRow struct {
	sourceID int16
	name     string
	reason   string
	by       string
}

func rejectsFromRecords(recs []pairRec, keyToID map[string]int16, st *ApplyReviewedStats) []rejectRow {
	var out []rejectRow
	for _, r := range recs {
		if r.Kind != "reject" || !r.Approve {
			continue
		}
		id, ok := keyToID[r.Source]
		if !ok || r.Name == "" {
			st.Errors++
			slog.Warn("malformed reject record", "source", r.Source, "name", r.Name)
			continue
		}
		out = append(out, rejectRow{sourceID: id, name: r.Name, reason: r.Reason, by: r.By})
	}
	return out
}

func tagIDExists(ctx context.Context, db *gorm.DB, id int64) bool {
	var n int64
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM catalog_tag WHERE id = ?`, id).Scan(&n).Error; err != nil {
		return false
	}
	return n > 0
}

func writeRejection(ctx context.Context, db *gorm.DB, r rejectRow, st *ApplyReviewedStats, inner *Stats) {
	res := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_name"}},
		DoNothing: true,
	}).Create(&model.CatalogTagRejection{SourceID: r.sourceID, SourceName: r.name, Reason: r.reason, RejectedBy: r.by})
	if res.Error != nil {
		inner.Errors++
		slog.Warn("write tag rejection", "source", r.sourceID, "name", r.name, "err", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		st.RejectsConflict++
		return
	}
	st.RejectsCreated++
}

func writeMemberMap(ctx context.Context, db *gorm.DB, m vocabEntry, tagID int64, st *Stats) {
	res := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_name"}},
		DoNothing: true,
	}).Create(&model.CatalogTagSourceMap{SourceID: m.SourceID, SourceName: m.Name, TagID: tagID})
	if res.Error != nil {
		st.Errors++
		slog.Warn("write single map", "source", m.SourceID, "name", m.Name, "err", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		st.MapsConflict++
		return
	}
	st.MapsCreated++
}

func stampTierKind(ctx context.Context, db *gorm.DB, tagID int64, tier, kind int16) int {
	humanTier := editspec.HumanFieldProvenanceSQL("field_provenance", "tier")
	humanKind := editspec.HumanFieldProvenanceSQL("field_provenance", "kind")
	res := db.WithContext(ctx).Exec(`UPDATE catalog_tag SET
		    tier = CASE WHEN COALESCE(`+humanTier+`, false) THEN tier ELSE ? END,
		    kind = CASE WHEN COALESCE(`+humanKind+`, false) THEN kind ELSE ? END,
		    updated_at = now()
		WHERE id = ? AND ((tier <> ? AND `+editspec.NotHumanFieldSQL("field_provenance", "tier")+`)
		               OR (kind <> ? AND `+editspec.NotHumanFieldSQL("field_provenance", "kind")+`))`,
		tier, kind, tagID, tier, kind)
	if res.Error != nil {
		slog.Warn("stamp tier/kind", "id", tagID, "err", res.Error)
		return 0
	}
	return int(res.RowsAffected)
}

type dsu struct{ parent map[string]string }

func newDSU() *dsu { return &dsu{parent: map[string]string{}} }

func (d *dsu) find(x string) string {
	p, ok := d.parent[x]
	if !ok {
		d.parent[x] = x
		return x
	}
	if p != x {
		d.parent[x] = d.find(p)
	}
	return d.parent[x]
}

func (d *dsu) union(a, b string) {
	ra, rb := d.find(a), d.find(b)
	if ra != rb {
		d.parent[ra] = rb
	}
}

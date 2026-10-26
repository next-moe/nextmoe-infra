package seriesorder

import (
	"context"
	"sort"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const (
	RelSequelOf    int64 = 2
	RelSideStoryOf int64 = 3
	RelFandiscOf   int64 = 4
	RelRemakeOf    int64 = 6
	RelSameSeries  int64 = 7
)

var SeriesEdgeTypes = []int64{RelSequelOf, RelSideStoryOf, RelFandiscOf, RelRemakeOf, RelSameSeries}

type Assignment struct {
	WorkID   int64
	Position int16
	Kind     int16
}

type Edge struct {
	A    int64
	B    int64
	Type int64
}

type Facts struct {
	releaseKey  map[int64]int64
	edgesByWork map[int64][]Edge
}

func LoadFacts(ctx context.Context, db *gorm.DB, workIDs []int64) (*Facts, error) {
	f := &Facts{releaseKey: map[int64]int64{}, edgesByWork: map[int64][]Edge{}}
	if len(workIDs) == 0 {
		return f, nil
	}
	var dates []struct {
		WorkID int64  `gorm:"column:work_id"`
		Key    *int64 `gorm:"column:key"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT r.work_id, `+model.WorkReleaseOrdSQL("r", "")+` AS key
		FROM catalog_release r
		WHERE r.work_id IN ? AND r.deleted_at IS NULL
		GROUP BY r.work_id`, workIDs).Scan(&dates).Error; err != nil {
		return nil, err
	}
	for _, d := range dates {
		if d.Key != nil {
			f.releaseKey[d.WorkID] = *d.Key
		}
	}

	var edges []Edge
	if err := db.WithContext(ctx).Raw(`
		SELECT r.a_work_id AS a, r.b_work_id AS b, r.relation_type_id AS type
		FROM catalog_work_relation r
		WHERE r.relation_type_id IN ? AND r.a_work_id IN ? AND r.b_work_id IN ?
		  AND `+editspec.NotSuppressedWorkRelationSQL("r"),
		SeriesEdgeTypes, workIDs, workIDs).Scan(&edges).Error; err != nil {
		return nil, err
	}
	for _, e := range edges {
		f.edgesByWork[e.A] = append(f.edgesByWork[e.A], e)
		f.edgesByWork[e.B] = append(f.edgesByWork[e.B], e)
	}
	return f, nil
}

func (f *Facts) Assign(workIDs []int64, fallbackKind int16) []Assignment {
	members := make(map[int64]struct{}, len(workIDs))
	for _, w := range workIDs {
		members[w] = struct{}{}
	}
	out := make([]Assignment, 0, len(members))
	for w := range members {
		out = append(out, Assignment{WorkID: w, Kind: f.kindOf(w, members, fallbackKind)})
	}
	sort.Slice(out, func(i, j int) bool {
		ki, oki := f.releaseKey[out[i].WorkID]
		kj, okj := f.releaseKey[out[j].WorkID]
		switch {
		case oki && okj && ki != kj:
			return ki < kj
		case oki != okj:
			return oki
		default:
			return out[i].WorkID < out[j].WorkID
		}
	})
	out = f.originalsBeforeRemakes(out)
	for i := range out {
		out[i].Position = int16(i + 1)
	}
	return out
}

func (f *Facts) originalsBeforeRemakes(sorted []Assignment) []Assignment {
	at := make(map[int64]Assignment, len(sorted))
	for _, a := range sorted {
		at[a.WorkID] = a
	}
	originals := map[int64][]int64{}
	for _, o := range sorted {
		for _, e := range f.edgesByWork[o.WorkID] {
			if e.Type != RelRemakeOf || e.B != o.WorkID {
				continue
			}
			if _, ok := at[e.A]; ok {
				originals[e.A] = append(originals[e.A], o.WorkID)
			}
		}
	}
	if len(originals) == 0 {
		return sorted
	}

	out := make([]Assignment, 0, len(sorted))
	seen := make(map[int64]bool, len(sorted))
	var place func(w int64)
	place = func(w int64) {
		if seen[w] {
			return
		}
		seen[w] = true
		for _, o := range originals[w] {
			place(o)
		}
		out = append(out, at[w])
	}
	for _, a := range sorted {
		place(a.WorkID)
	}
	return out
}

func (f *Facts) kindOf(workID int64, members map[int64]struct{}, fallbackKind int16) int16 {
	var fandisc, sideStory, edged bool
	for _, e := range f.edgesByWork[workID] {
		other := e.A
		if e.A == workID {
			other = e.B
		}
		if _, ok := members[other]; !ok {
			continue
		}
		edged = true
		if e.A != workID {
			continue
		}
		switch e.Type {
		case RelFandiscOf:
			fandisc = true
		case RelSideStoryOf:
			sideStory = true
		}
	}
	switch {
	case fandisc:
		return model.SeriesMemberKindFandisc
	case sideStory:
		return model.SeriesMemberKindSideStory
	case edged:
		return model.SeriesMemberKindMain
	default:
		return fallbackKind
	}
}

package derivedseries

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"api/internal/infrastructure/database"
	"api/internal/jobs/seriesorder"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

const derivedSourceKey = "derived"

var ownedSourceKeys = []string{"dlsite", "curated"}

type Opts struct {
	Apply    bool
	DSN      string
	Receipts string
	Worklist string
}

type Stats struct {
	Works          int
	Edges          int
	Components     int
	SkippedOverlap int
	SkippedGiant   int
	SkippedNoName  int
	Bridges        int
	BridgeEdgesCut int
	GiantsSplit    int
	Eligible       int
	MembersWanted  int

	SeriesCreated int
	SeriesRenamed int
	SeriesDeleted int
	MembersAdded  int
	MembersStale  int
	OrderChanged  int
	TouchedWorks  int

	NamedByPrefix   int
	NamedByFallback int
	NamedByOverride int
	OverridesStale  int
}

type receipt struct {
	ExternalID  string `json:"external_id"`
	DisplayName string `json:"display_name"`
	NamedBy     string `json:"named_by"`
	SeriesID    int64  `json:"series_id,omitempty"`
	Members     []struct {
		WorkID   int64  `json:"work_id"`
		Position int16  `json:"position"`
		Kind     int16  `json:"kind"`
		Title    string `json:"title"`
	} `json:"members"`
}

type worklistEntry struct {
	Reason       string  `json:"reason"`
	ExternalID   string  `json:"external_id"`
	WorkIDs      []int64 `json:"work_ids"`
	Size         int     `json:"size"`
	HitSeriesIDs []int64 `json:"hit_series_ids,omitempty"`
}

type candidate struct {
	externalID string
	works      []int64
	name       string
	namedBy    string
}

func Run(ctx context.Context, opts Opts) (*Stats, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("catalog DSN is required (--dsn); refusing to guess")
	}
	db, err := database.OpenJob(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect catalog db: %w", err)
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}
	return RunWithDB(ctx, db, opts)
}

func RunWithDB(ctx context.Context, db *gorm.DB, opts Opts) (*Stats, error) {
	derivedSrc, err := resolveSource(ctx, db, derivedSourceKey)
	if err != nil {
		return nil, err
	}
	st := &Stats{}

	var rawEdges []seriesorder.Edge
	if err := db.WithContext(ctx).Raw(`
		SELECT r.a_work_id AS a, r.b_work_id AS b, r.relation_type_id AS type
		FROM catalog_work_relation r
		JOIN catalog_work wa ON wa.id = r.a_work_id AND wa.deleted_at IS NULL
		JOIN catalog_work wb ON wb.id = r.b_work_id AND wb.deleted_at IS NULL
		WHERE r.relation_type_id IN ? AND `+editspec.NotSuppressedWorkRelationSQL("r"), seriesorder.SeriesEdgeTypes).Scan(&rawEdges).Error; err != nil {
		return nil, fmt.Errorf("load edges: %w", err)
	}
	st.Edges = len(rawEdges)
	edgesByWork := map[int64][]seriesorder.Edge{}
	workSet := map[int64]struct{}{}
	for _, e := range rawEdges {
		edgesByWork[e.A] = append(edgesByWork[e.A], e)
		edgesByWork[e.B] = append(edgesByWork[e.B], e)
		workSet[e.A] = struct{}{}
		workSet[e.B] = struct{}{}
	}
	works := make([]int64, 0, len(workSet))
	for w := range workSet {
		works = append(works, w)
	}
	st.Works = len(works)

	owned, err := ownedMembership(ctx, db)
	if err != nil {
		return nil, err
	}

	var wl *os.File
	if opts.Worklist != "" {
		if wl, err = os.Create(opts.Worklist); err != nil {
			return nil, fmt.Errorf("worklist: %w", err)
		}
		defer wl.Close()
	}
	wlEnc := json.NewEncoder(wl)
	emit := func(e worklistEntry) error {
		if wl == nil {
			return nil
		}
		return wlEnc.Encode(e)
	}

	titles, err := workTitles(ctx, db, works)
	if err != nil {
		return nil, err
	}

	bridges := crossoverBridges(works, edgesByWork, titles)
	st.Bridges = len(bridges)
	for _, b := range bridges {
		if err := emit(worklistEntry{Reason: "crossover", ExternalID: fmt.Sprintf("work:%d", b),
			WorkIDs: []int64{b}, Size: 1}); err != nil {
			return nil, fmt.Errorf("worklist: %w", err)
		}
	}
	edgesByWork, st.BridgeEdgesCut = pruneBridgeEdges(edgesByWork, bridges)

	comps := components(works, edgesByWork, nil)
	st.Components = len(comps)
	var kept []component
	for _, c := range comps {
		if hits := overlapping(c, owned); len(hits) > 0 {
			st.SkippedOverlap++
			if err := emit(worklistEntry{Reason: "overlap", ExternalID: externalID(c),
				WorkIDs: c, Size: len(c), HitSeriesIDs: hits}); err != nil {
				return nil, fmt.Errorf("worklist: %w", err)
			}
			continue
		}
		if len(c) < GiantSize {
			kept = append(kept, c)
			continue
		}
		var rescued []component
		for _, sub := range splitGiant(c, edgesByWork) {
			if len(sub) >= GiantSize {
				continue
			}
			rescued = append(rescued, sub)
		}
		st.GiantsSplit++
		kept = append(kept, rescued...)
		st.SkippedGiant++
		if err := emit(worklistEntry{Reason: "giant", ExternalID: externalID(c),
			WorkIDs: c, Size: len(c)}); err != nil {
			return nil, fmt.Errorf("worklist: %w", err)
		}
	}

	var allMembers []int64
	for _, c := range kept {
		allMembers = append(allMembers, c...)
	}
	facts, err := seriesorder.LoadFacts(ctx, db, allMembers)
	if err != nil {
		return nil, fmt.Errorf("load ordering facts: %w", err)
	}
	want := make(map[string]*candidate, len(kept))
	for _, c := range kept {
		names := make([]string, 0, len(c))
		for _, w := range c {
			names = append(names, titles[w])
		}
		ordered := facts.Assign(c, model.SeriesMemberKindMain)
		name, byPrefix := nameComponent(names, titles[ordered[0].WorkID])
		if name == "" {
			st.SkippedNoName++
			if err := emit(worklistEntry{Reason: "no_name", ExternalID: externalID(c),
				WorkIDs: c, Size: len(c)}); err != nil {
				return nil, fmt.Errorf("worklist: %w", err)
			}
			continue
		}
		cand := &candidate{externalID: externalID(c), works: c, name: name, namedBy: "fallback"}
		if byPrefix {
			cand.namedBy = "prefix"
			st.NamedByPrefix++
		} else {
			st.NamedByFallback++
		}
		want[cand.externalID] = cand
		st.MembersWanted += len(c)
	}
	st.Eligible = len(want)

	if err := applyOverrides(ctx, db, derivedSrc, want, opts, st); err != nil {
		return nil, err
	}

	touched, err := reconcile(ctx, db, derivedSrc, want, facts, titles, opts, st)
	if err != nil {
		return nil, err
	}
	if opts.Apply && len(touched) > 0 {
		if err := repository.TouchWorks(ctx, db, touched); err != nil {
			return nil, fmt.Errorf("touch works: %w", err)
		}
		st.TouchedWorks = len(dedupe(touched))
	}
	return st, nil
}

func externalID(c component) string { return fmt.Sprintf("comp:%d", c[0]) }

func overlapping(c component, owned map[int64][]int64) []int64 {
	seen := map[int64]struct{}{}
	var out []int64
	for _, w := range c {
		for _, s := range owned[w] {
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func ownedMembership(ctx context.Context, db *gorm.DB) (map[int64][]int64, error) {
	var rows []struct {
		WorkID   int64 `gorm:"column:work_id"`
		SeriesID int64 `gorm:"column:series_id"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT m.work_id, m.series_id
		FROM catalog_series_member m
		JOIN catalog_series s ON s.id = m.series_id
		JOIN catalog_source src ON src.id = s.source_id
		WHERE src.key IN ?`, ownedSourceKeys).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load existing memberships: %w", err)
	}
	out := make(map[int64][]int64, len(rows))
	for _, r := range rows {
		out[r.WorkID] = append(out[r.WorkID], r.SeriesID)
	}
	return out, nil
}

func workTitles(ctx context.Context, db *gorm.DB, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID          int64  `gorm:"column:id"`
		DisplayName string `gorm:"column:display_name"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT id, display_name FROM catalog_work WHERE id IN ?`, ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load work titles: %w", err)
	}
	for _, r := range rows {
		out[r.ID] = r.DisplayName
	}
	return out, nil
}

func resolveSource(ctx context.Context, db *gorm.DB, key string) (int16, error) {
	var id int16
	if err := db.WithContext(ctx).Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).
		Scan(&id).Error; err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, fmt.Errorf("catalog_source has no %q row (run the catalog migration/seed first)", key)
	}
	return id, nil
}

func dedupe(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

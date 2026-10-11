package tagparents

import (
	"context"
	"fmt"
	"log/slog"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const (
	OpAdd    = "add"
	OpRemove = "remove"
)

type CuratedRec struct {
	TagID    int64  `json:"tag_id"`
	Tag      string `json:"tag"`
	ParentID int64  `json:"parent_id"`
	Parent   string `json:"parent"`
	Op       string `json:"op"`
	Reason   string `json:"reason"`
}

type CuratedStats struct {
	Records   int
	Added     int
	Endorsed  int
	Removed   int
	Unchanged int
	Errors    int
}

func ApplyCurated(ctx context.Context, db *gorm.DB, recs []CuratedRec, apply bool) (*CuratedStats, error) {
	all := map[Edge]bool{}
	for _, origin := range []int16{model.TagParentOriginVNDB, model.TagParentOriginCurated} {
		edges, err := loadEdges(ctx, db, origin)
		if err != nil {
			return nil, err
		}
		for e := range edges {
			all[e] = true
		}
	}
	g := newGraph(all)
	st := &CuratedStats{Records: len(recs)}
	for _, r := range recs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err := applyOne(ctx, db, g, r, apply, st); err != nil {
			st.Errors++
			slog.Warn("curated tag parent refused", "tag_id", r.TagID, "tag", r.Tag,
				"parent_id", r.ParentID, "parent", r.Parent, "op", r.Op, "err", err)
		}
	}
	return st, nil
}

func applyOne(ctx context.Context, db *gorm.DB, g *graph, r CuratedRec, apply bool, st *CuratedStats) error {
	if r.TagID <= 0 || r.ParentID <= 0 || r.Tag == "" || r.Parent == "" {
		return fmt.Errorf("malformed record (need tag_id, tag, parent_id, parent)")
	}
	if r.TagID == r.ParentID {
		return fmt.Errorf("a tag cannot be its own parent")
	}
	if r.Op != OpAdd && r.Op != OpRemove {
		return fmt.Errorf("op must be %q or %q", OpAdd, OpRemove)
	}
	if r.Op == OpAdd && r.Reason == "" {
		return fmt.Errorf("an added edge needs a reason")
	}
	for id, want := range map[int64]string{r.TagID: r.Tag, r.ParentID: r.Parent} {
		var names []string
		if err := db.WithContext(ctx).Raw(`SELECT name FROM catalog_tag WHERE id = ?`, id).Scan(&names).Error; err != nil {
			return err
		}
		if len(names) == 0 {
			return fmt.Errorf("tag %d does not exist", id)
		}
		if names[0] != want {
			return fmt.Errorf("tag %d is named %q, the record says %q", id, names[0], want)
		}
	}
	var origins []int16
	if err := db.WithContext(ctx).Raw(`SELECT origin FROM catalog_tag_parent WHERE tag_id = ? AND parent_id = ?`,
		r.TagID, r.ParentID).Scan(&origins).Error; err != nil {
		return err
	}
	e := Edge{Tag: r.TagID, Parent: r.ParentID}

	if r.Op == OpRemove {
		switch {
		case len(origins) == 0:
			st.Unchanged++
			return nil
		case origins[0] == model.TagParentOriginVNDB:
			return fmt.Errorf("the edge comes from VNDB; the next sync would restore it")
		}
		st.Removed++
		if !apply {
			return nil
		}
		return db.WithContext(ctx).Exec(`DELETE FROM catalog_tag_parent WHERE tag_id = ? AND parent_id = ? AND origin = ?`,
			r.TagID, r.ParentID, model.TagParentOriginCurated).Error
	}

	if len(origins) > 0 {
		if origins[0] == model.TagParentOriginCurated {
			st.Unchanged++
			return nil
		}
		st.Endorsed++
		if !apply {
			return nil
		}
		return db.WithContext(ctx).Exec(`UPDATE catalog_tag_parent SET origin = ?, note = ? WHERE tag_id = ? AND parent_id = ?`,
			model.TagParentOriginCurated, r.Reason, r.TagID, r.ParentID).Error
	}
	if g.reaches(r.ParentID, r.TagID) {
		return fmt.Errorf("parent %d already descends from %d; the edge would close a loop", r.ParentID, r.TagID)
	}
	g.add(e)
	st.Added++
	if !apply {
		return nil
	}
	return db.WithContext(ctx).Create(&model.CatalogTagParent{
		TagID: r.TagID, ParentID: r.ParentID, Origin: model.TagParentOriginCurated, Note: r.Reason,
	}).Error
}

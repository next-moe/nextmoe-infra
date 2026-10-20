package tagparents

import (
	"context"
	"fmt"
	"sort"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/vndbtagmap"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var tagMap = vndbtagmap.Embedded

type Edge struct {
	Tag    int64
	Parent int64
}

type SyncStats struct {
	VNDBTags   int
	Mapped     int
	Edges      int
	Added      int
	Deleted    int
	Cycles     int
	Suppressed int
}

func Sync(ctx context.Context, db *gorm.DB, apply bool) (*SyncStats, error) {
	st := &SyncStats{}
	want, err := projectVNDB(ctx, db, st)
	if err != nil {
		return nil, err
	}
	if err := dropSuppressed(ctx, db, want, st); err != nil {
		return nil, err
	}
	have, err := loadEdges(ctx, db, model.TagParentOriginVNDB)
	if err != nil {
		return nil, err
	}
	curated, err := loadEdges(ctx, db, model.TagParentOriginCurated)
	if err != nil {
		return nil, err
	}

	g := newGraph(curated)
	keep := map[Edge]bool{}
	for _, e := range sortedEdges(want) {
		if g.reaches(e.Parent, e.Tag) {
			st.Cycles++
			continue
		}
		g.add(e)
		keep[e] = true
	}
	st.Edges = len(keep)

	var toAdd, toDel []Edge
	for e := range keep {
		if !have[e] && !curated[e] {
			toAdd = append(toAdd, e)
		}
	}
	for e := range have {
		if !keep[e] {
			toDel = append(toDel, e)
		}
	}
	st.Added, st.Deleted = len(toAdd), len(toDel)
	if !apply || (len(toAdd) == 0 && len(toDel) == 0) {
		return st, nil
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, e := range toDel {
			if err := tx.Exec(`DELETE FROM catalog_tag_parent WHERE tag_id = ? AND parent_id = ? AND origin = ?`,
				e.Tag, e.Parent, model.TagParentOriginVNDB).Error; err != nil {
				return err
			}
		}
		rows := make([]model.CatalogTagParent, 0, len(toAdd))
		for _, e := range toAdd {
			rows = append(rows, model.CatalogTagParent{TagID: e.Tag, ParentID: e.Parent, Origin: model.TagParentOriginVNDB})
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 500).Error
	})
	if err != nil {
		return nil, fmt.Errorf("write tag parents: %w", err)
	}
	return st, nil
}

func dropSuppressed(ctx context.Context, db *gorm.DB, want map[Edge]bool, st *SyncStats) error {
	var rows []Edge
	if err := db.WithContext(ctx).Raw(`
		SELECT entity_id AS tag, split_part(identity_key, ':', 2)::bigint AS parent
		FROM edit_suppressed_row WHERE entity_type = ? AND field_key = ?`,
		editspec.TypeTag, editspec.FieldTagParents).Scan(&rows).Error; err != nil {
		return fmt.Errorf("load suppressed tag parents: %w", err)
	}
	for _, e := range rows {
		if want[e] {
			delete(want, e)
			st.Suppressed++
		}
	}
	return nil
}

func projectVNDB(ctx context.Context, db *gorm.DB, st *SyncStats) (map[Edge]bool, error) {
	var tags []struct {
		ID    string `gorm:"column:id"`
		Name  string `gorm:"column:name"`
		Alias string `gorm:"column:alias"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, name, coalesce(alias, '') AS alias FROM src_vndb.tags`).
		Scan(&tags).Error; err != nil {
		return nil, fmt.Errorf("load src_vndb.tags: %w", err)
	}
	var ups []struct {
		ID     string `gorm:"column:id"`
		Parent string `gorm:"column:parent"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT id, parent FROM src_vndb.tags_parents`).Scan(&ups).Error; err != nil {
		return nil, fmt.Errorf("load src_vndb.tags_parents: %w", err)
	}
	var maps []struct {
		SourceName string `gorm:"column:source_name"`
		TagID      int64  `gorm:"column:tag_id"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT m.source_name, m.tag_id FROM catalog_tag_source_map m
		JOIN catalog_source s ON s.id = m.source_id WHERE s.key = 'vndb'`).Scan(&maps).Error; err != nil {
		return nil, fmt.Errorf("load vndb tag map: %w", err)
	}
	st.VNDBTags = len(tags)

	byName := make(map[string]int64, len(maps))
	for _, m := range maps {
		byName[m.SourceName] = m.TagID
	}
	tm := tagMap()
	canon := make(map[string]int64, len(tags))
	for _, t := range tags {
		if id, ok := byName[vndbtagmap.Resolve(tm, t.Name, t.Alias)]; ok {
			canon[t.ID] = id
		}
	}
	st.Mapped = len(canon)

	parentsOf := make(map[string][]string, len(ups))
	for _, u := range ups {
		parentsOf[u.ID] = append(parentsOf[u.ID], u.Parent)
	}
	want := map[Edge]bool{}
	for gid, c := range canon {
		seen := map[string]bool{}
		stack := append([]string(nil), parentsOf[gid]...)
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[p] {
				continue
			}
			seen[p] = true
			if cp, ok := canon[p]; ok && cp != c {
				want[Edge{Tag: c, Parent: cp}] = true
				continue
			}
			stack = append(stack, parentsOf[p]...)
		}
	}
	return want, nil
}

func loadEdges(ctx context.Context, db *gorm.DB, origin int16) (map[Edge]bool, error) {
	var rows []struct {
		TagID    int64 `gorm:"column:tag_id"`
		ParentID int64 `gorm:"column:parent_id"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT tag_id, parent_id FROM catalog_tag_parent WHERE origin = ?`, origin).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load tag parents: %w", err)
	}
	out := make(map[Edge]bool, len(rows))
	for _, r := range rows {
		out[Edge{Tag: r.TagID, Parent: r.ParentID}] = true
	}
	return out, nil
}

func sortedEdges(set map[Edge]bool) []Edge {
	out := make([]Edge, 0, len(set))
	for e := range set {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tag != out[j].Tag {
			return out[i].Tag < out[j].Tag
		}
		return out[i].Parent < out[j].Parent
	})
	return out
}

type graph struct{ up map[int64][]int64 }

func newGraph(edges map[Edge]bool) *graph {
	g := &graph{up: map[int64][]int64{}}
	for e := range edges {
		g.add(e)
	}
	return g
}

func (g *graph) add(e Edge) { g.up[e.Tag] = append(g.up[e.Tag], e.Parent) }

func (g *graph) reaches(from, target int64) bool {
	seen := map[int64]bool{}
	stack := []int64{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == target {
			return true
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		stack = append(stack, g.up[n]...)
	}
	return false
}

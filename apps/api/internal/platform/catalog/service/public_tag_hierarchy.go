package service

import (
	"cmp"
	"context"
	"slices"

	"api/internal/platform/catalog/dto"
)

type tagHierarchy struct {
	parents     map[int64][]dto.PublicTagRef
	children    map[int64]int
	descendants map[int64]int
}

func (s *PublicService) tagHierarchyFor(ctx context.Context, ids []int64) (tagHierarchy, error) {
	h := tagHierarchy{
		parents:     make(map[int64][]dto.PublicTagRef, len(ids)),
		children:    make(map[int64]int, len(ids)),
		descendants: make(map[int64]int, len(ids)),
	}
	if len(ids) == 0 {
		return h, nil
	}
	var parents []struct {
		TagID  int64 `gorm:"column:tag_id"`
		ID     int64
		Name   string
		Sexual bool
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT p.tag_id, t.id, t.name, t.sexual FROM catalog_tag_parent p
		JOIN catalog_tag t ON t.id = p.parent_id
		WHERE p.tag_id IN ? ORDER BY p.tag_id, t.id`, ids).Scan(&parents).Error; err != nil {
		return h, err
	}
	for _, p := range parents {
		h.parents[p.TagID] = append(h.parents[p.TagID], dto.PublicTagRef{ID: p.ID, Name: p.Name, Sexual: p.Sexual})
	}
	var counts []struct {
		ID int64
		N  int
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT parent_id AS id, count(*) AS n FROM catalog_tag_parent
		WHERE parent_id IN ? GROUP BY parent_id`, ids).Scan(&counts).Error; err != nil {
		return h, err
	}
	for _, c := range counts {
		h.children[c.ID] = c.N
	}
	counts = counts[:0]
	if err := s.db.WithContext(ctx).Raw(`
		WITH RECURSIVE d(root, id) AS (
			SELECT parent_id, tag_id FROM catalog_tag_parent WHERE parent_id IN ?
			UNION
			SELECT d.root, p.tag_id FROM catalog_tag_parent p JOIN d ON p.parent_id = d.id
		)
		SELECT root AS id, count(*) AS n FROM d WHERE id <> root GROUP BY root`, ids).Scan(&counts).Error; err != nil {
		return h, err
	}
	for _, c := range counts {
		h.descendants[c.ID] = c.N
	}
	return h, nil
}

func (s *PublicService) tagChildren(ctx context.Context, id int64) ([]dto.PublicTagRef, error) {
	out := []dto.PublicTagRef{}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT t.id, t.name, t.sexual FROM catalog_tag_parent p
		JOIN catalog_tag t ON t.id = p.tag_id
		WHERE p.parent_id = ? ORDER BY t.id`, id).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PublicService) TagDescendants(ctx context.Context, id int64) ([]dto.PublicTagNode, error) {
	return s.tagClosure(ctx, id, "parent_id", "tag_id")
}

func (s *PublicService) TagAncestors(ctx context.Context, id int64) ([]dto.PublicTagNode, error) {
	return s.tagClosure(ctx, id, "tag_id", "parent_id")
}

func (s *PublicService) tagClosure(ctx context.Context, id int64, from, to string) ([]dto.PublicTagNode, error) {
	var edges []struct {
		TagID    int64 `gorm:"column:tag_id"`
		ParentID int64 `gorm:"column:parent_id"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		WITH RECURSIVE c(id) AS (
			SELECT `+to+` FROM catalog_tag_parent WHERE `+from+` = ?
			UNION
			SELECT p.`+to+` FROM catalog_tag_parent p JOIN c ON p.`+from+` = c.id
		)
		SELECT p.tag_id, p.parent_id FROM catalog_tag_parent p
		WHERE p.`+to+` IN (SELECT id FROM c) AND (p.`+from+` = ? OR p.`+from+` IN (SELECT id FROM c))`,
		id, id).Scan(&edges).Error; err != nil {
		return nil, err
	}
	next := make(map[int64][]int64, len(edges))
	parents := make(map[int64][]int64, len(edges))
	for _, e := range edges {
		near, far := e.ParentID, e.TagID
		if from == "tag_id" {
			near, far = e.TagID, e.ParentID
		}
		next[near] = append(next[near], far)
		if e.TagID != id {
			parents[e.TagID] = append(parents[e.TagID], e.ParentID)
		}
	}
	depth := map[int64]int{id: 0}
	queue := []int64{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, n := range next[cur] {
			if _, seen := depth[n]; !seen {
				depth[n] = depth[cur] + 1
				queue = append(queue, n)
			}
		}
	}
	delete(depth, id)
	out := []dto.PublicTagNode{}
	if len(depth) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(depth))
	for n := range depth {
		ids = append(ids, n)
	}
	var refs []dto.PublicTagRef
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id, name, sexual FROM catalog_tag WHERE id IN ?`, ids).Scan(&refs).Error; err != nil {
		return nil, err
	}
	for _, r := range refs {
		p := append([]int64{}, parents[r.ID]...)
		slices.Sort(p)
		out = append(out, dto.PublicTagNode{ID: r.ID, Name: r.Name, Sexual: r.Sexual, ParentIDs: slices.Compact(p)})
	}
	slices.SortFunc(out, func(a, b dto.PublicTagNode) int {
		if d := depth[a.ID] - depth[b.ID]; d != 0 {
			return d
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return out, nil
}

func (h tagHierarchy) parentsOf(id int64) []dto.PublicTagRef {
	if p := h.parents[id]; p != nil {
		return p
	}
	return []dto.PublicTagRef{}
}

func (s *PublicService) TagExclusion(ctx context.Context, exact, trees []int64) ([]int64, error) {
	set := make(map[int64]bool, len(exact)+len(trees))
	for _, id := range exact {
		set[id] = true
	}
	if len(trees) > 0 {
		var ids []int64
		if err := s.db.WithContext(ctx).Raw(`
			WITH RECURSIVE d(id) AS (
				SELECT id FROM catalog_tag WHERE id IN ?
				UNION
				SELECT p.tag_id FROM catalog_tag_parent p JOIN d ON p.parent_id = d.id
			)
			SELECT id FROM d`, trees).Scan(&ids).Error; err != nil {
			return nil, err
		}
		for _, id := range ids {
			set[id] = true
		}
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	slices.Sort(out)
	return out, nil
}

func tagExclusionWhere(workCol string, ids []int64) (string, []any) {
	if len(ids) == 0 {
		return "", nil
	}
	return `NOT EXISTS (SELECT 1 FROM catalog_work_tag xwt
		JOIN catalog_tag_source_map xm ON xm.source_id = xwt.source_id AND xm.source_name = xwt.name
		WHERE xwt.work_id = ` + workCol + ` AND xm.tag_id IN ?)`, []any{ids}
}

func (s *PublicService) ExcludedWorkIDs(ctx context.Context, tagIDs []int64) ([]int64, error) {
	out := []int64{}
	if len(tagIDs) == 0 {
		return out, nil
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT DISTINCT wt.work_id FROM catalog_work_tag wt
		JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name
		JOIN catalog_work w ON w.id = wt.work_id AND w.deleted_at IS NULL
		WHERE m.tag_id IN ?
		ORDER BY wt.work_id`, tagIDs).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

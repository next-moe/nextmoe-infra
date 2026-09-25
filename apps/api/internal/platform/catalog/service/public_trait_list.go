package service

import (
	"context"
	"slices"

	"api/internal/platform/catalog/dto"
)

type TraitListFilter struct {
	ParentID int64
	GroupID  int64
	Root     *bool
}

type TraitRefRow struct {
	ID          int64
	DisplayName string
	Localized   map[string]dto.PublicLocalizedName
}

type TraitPathRow struct {
	Group   *TraitRefRow
	Parents []TraitRefRow
}

func (s *PublicService) TraitsList(ctx context.Context, ids []int64, cursor string, limit int, nsfw bool, filter TraitListFilter, include []string) (EntityListPage, error) {
	if err := s.refuseSexualTrait(ctx, filter.ParentID, nsfw, "parent_id"); err != nil {
		return EntityListPage{}, err
	}
	if err := s.refuseSexualTrait(ctx, filter.GroupID, nsfw, "group_id"); err != nil {
		return EntityListPage{}, err
	}
	spec := entityListSpec{
		lane:  taxonomyLaneTraits,
		table: "catalog_character_trait",
		selectSQL: "id, name AS display_name, name_zh, name_zh_provenance, vndb_tid, " +
			"sexual_family AS sexual, searchable, applicable, gorder, group_tid, alias, description",
		ids: ids, cursor: cursor, limit: limit,
	}
	if !nsfw {
		spec.extraWhere = append(spec.extraWhere, "NOT sexual_family")
	}
	if filter.ParentID > 0 {
		spec.extraWhere = append(spec.extraWhere, "id IN (SELECT trait_id FROM catalog_character_trait_parent WHERE parent_id = ?)")
		spec.extraArgs = append(spec.extraArgs, filter.ParentID)
	}
	if filter.GroupID > 0 {
		spec.extraWhere = append(spec.extraWhere, "group_tid = (SELECT vndb_tid FROM catalog_character_trait WHERE id = ?) AND id <> ?")
		spec.extraArgs = append(spec.extraArgs, filter.GroupID, filter.GroupID)
	}
	if filter.Root != nil {
		if *filter.Root {
			spec.extraWhere = append(spec.extraWhere, "group_tid = ''")
		} else {
			spec.extraWhere = append(spec.extraWhere, "group_tid <> ''")
		}
	}
	page, err := s.entityIDList(ctx, spec)
	if err != nil {
		return EntityListPage{}, err
	}
	if err := s.attachTraitGraph(ctx, page.Items, nsfw, include); err != nil {
		return EntityListPage{}, err
	}
	return page, nil
}

func (s *PublicService) Trait(ctx context.Context, id int64, nsfw bool, include []string) (EntityListRow, bool, error) {
	page, err := s.TraitsList(ctx, []int64{id}, "", 1, nsfw, TraitListFilter{}, include)
	if err != nil {
		return EntityListRow{}, false, err
	}
	if len(page.Items) == 0 {
		return EntityListRow{}, false, nil
	}
	return page.Items[0], true, nil
}

func (s *PublicService) refuseSexualTrait(ctx context.Context, id int64, nsfw bool, param string) error {
	if id == 0 || nsfw {
		return nil
	}
	var row struct {
		ID           int64
		SexualFamily bool `gorm:"column:sexual_family"`
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id, sexual_family FROM catalog_character_trait WHERE id = ?`, id).
		Scan(&row).Error; err != nil {
		return err
	}
	if row.ID != 0 && row.SexualFamily {
		return &SexualTraitError{ID: id, Parameter: param}
	}
	return nil
}

func (s *PublicService) attachTraitGraph(ctx context.Context, rows []EntityListRow, nsfw bool, include []string) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		rows[i].Localized = traitLocalized(r.NameZh, r.NameZhProv)
		if r.GroupTID == "" {
			n := int(r.GOrder)
			rows[i].RootOrder = &n
		}
		wantAliases := slices.Contains(include, "aliases")
		wantDesc := slices.Contains(include, "description")
		if wantAliases {
			rows[i].TraitAliases = SplitTraitAliases(r.Alias)
		}
		if wantDesc {
			d := PlainTraitDescription(r.Description)
			rows[i].TraitDescription = &d
		}
	}
	groups, parents, err := s.traitParentGroupRows(ctx, ids)
	if err != nil {
		return err
	}
	counts, err := s.traitChildCounts(ctx, ids, nsfw)
	if err != nil {
		return err
	}
	for i := range rows {
		id := rows[i].ID
		if g, ok := groups[id]; ok {
			cp := g
			rows[i].Group = &cp
		}
		if p := parents[id]; p != nil {
			rows[i].Parents = p
		} else {
			rows[i].Parents = []TraitRefRow{}
		}
		rows[i].ChildCount = counts[id]
	}
	return nil
}

func (s *PublicService) TraitPaths(ctx context.Context, ids []int64) (map[int64]TraitPathRow, error) {
	out := make(map[int64]TraitPathRow, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	groups, parents, err := s.traitParentGroupRows(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		path := TraitPathRow{Parents: []TraitRefRow{}}
		if g, ok := groups[id]; ok {
			cp := g
			path.Group = &cp
		}
		if p := parents[id]; p != nil {
			path.Parents = p
		}
		out[id] = path
	}
	return out, nil
}

func (s *PublicService) traitParentGroupRows(ctx context.Context, ids []int64) (groups map[int64]TraitRefRow, parents map[int64][]TraitRefRow, err error) {
	groups = map[int64]TraitRefRow{}
	parents = map[int64][]TraitRefRow{}
	type refScan struct {
		TraitID    int64  `gorm:"column:trait_id"`
		ID         int64  `gorm:"column:id"`
		Name       string `gorm:"column:name"`
		NameZh     string `gorm:"column:name_zh"`
		NameZhProv int16  `gorm:"column:name_zh_provenance"`
	}
	var groupRows []refScan
	if err := s.db.WithContext(ctx).Raw(`
		SELECT t.id AS trait_id, g.id, g.name, g.name_zh, g.name_zh_provenance
		FROM catalog_character_trait t
		JOIN catalog_character_trait g ON g.vndb_tid = t.group_tid
		WHERE t.id IN ? AND t.group_tid <> ''`, ids).Scan(&groupRows).Error; err != nil {
		return nil, nil, err
	}
	for _, r := range groupRows {
		groups[r.TraitID] = TraitRefRow{
			ID: r.ID, DisplayName: r.Name, Localized: traitLocalized(r.NameZh, r.NameZhProv),
		}
	}
	var parentRows []refScan
	if err := s.db.WithContext(ctx).Raw(`
		SELECT p.trait_id, par.id, par.name, par.name_zh, par.name_zh_provenance
		FROM catalog_character_trait_parent p
		JOIN catalog_character_trait par ON par.id = p.parent_id
		WHERE p.trait_id IN ?
		ORDER BY p.trait_id, par.id`, ids).Scan(&parentRows).Error; err != nil {
		return nil, nil, err
	}
	for _, r := range parentRows {
		parents[r.TraitID] = append(parents[r.TraitID], TraitRefRow{
			ID: r.ID, DisplayName: r.Name, Localized: traitLocalized(r.NameZh, r.NameZhProv),
		})
	}
	return groups, parents, nil
}

func (s *PublicService) traitChildCounts(ctx context.Context, ids []int64, nsfw bool) (map[int64]int, error) {
	var rows []struct {
		ID int64 `gorm:"column:id"`
		N  int   `gorm:"column:n"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT p.parent_id AS id, count(*)::int AS n
		FROM catalog_character_trait_parent p
		JOIN catalog_character_trait c ON c.id = p.trait_id
		WHERE p.parent_id IN ? AND (? OR NOT c.sexual_family)
		GROUP BY p.parent_id`, ids, nsfw).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]int, len(rows))
	for _, r := range rows {
		out[r.ID] = r.N
	}
	return out, nil
}

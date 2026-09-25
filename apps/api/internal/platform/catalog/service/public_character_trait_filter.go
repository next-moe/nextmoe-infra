package service

import (
	"context"

	"api/internal/platform/catalog/model"
)

type CharacterTraitFilter struct {
	TraitIDs []int64
	MatchAny bool
}

type SexualTraitError struct {
	ID int64
}

func (e *SexualTraitError) Error() string {
	return "trait is sexual; nsfw=true is required"
}

const characterTraitExistsSQL = `EXISTS (SELECT 1 FROM catalog_character_trait_link l WHERE l.character_id = catalog_character.id AND l.trait_id IN ? AND l.spoiler_level <= ?)`

func (s *PublicService) expandCharacterTraits(ctx context.Context, ids []int64, nsfw bool) ([][]int64, error) {
	type row struct {
		RootID  int64 `gorm:"column:root_id"`
		TraitID int64 `gorm:"column:trait_id"`
		Sexual  bool
	}
	var rows []row
	if err := s.db.WithContext(ctx).Raw(`
		WITH RECURSIVE tree(root_id, trait_id) AS (
			SELECT id, id FROM catalog_character_trait WHERE id IN ?
			UNION
			SELECT tree.root_id, p.trait_id
			FROM catalog_character_trait_parent p
			INNER JOIN tree ON p.parent_id = tree.trait_id
		)
		SELECT tree.root_id, tree.trait_id, t.sexual
		FROM tree
		JOIN catalog_character_trait t ON t.id = tree.trait_id`,
		ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	namedPresent := make(map[int64]bool, len(ids))
	namedSexual := make(map[int64]bool, len(ids))
	byRoot := make(map[int64][]int64, len(ids))
	for _, r := range rows {
		if r.TraitID == r.RootID {
			namedPresent[r.RootID] = true
			namedSexual[r.RootID] = r.Sexual
		}
		if !nsfw && r.Sexual {
			continue
		}
		byRoot[r.RootID] = append(byRoot[r.RootID], r.TraitID)
	}
	for _, id := range ids {
		if namedPresent[id] && namedSexual[id] && !nsfw {
			return nil, &SexualTraitError{ID: id}
		}
	}
	sets := make([][]int64, len(ids))
	for i, id := range ids {
		sets[i] = byRoot[id]
	}
	return sets, nil
}

func characterTraitWhere(sets [][]int64, matchAny bool) (where []string, args []any, empty bool) {
	if matchAny {
		var union []int64
		seen := map[int64]struct{}{}
		for _, set := range sets {
			for _, id := range set {
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				union = append(union, id)
			}
		}
		if len(union) == 0 {
			return nil, nil, true
		}
		return []string{characterTraitExistsSQL}, []any{union, model.SpoilerNone}, false
	}
	for _, set := range sets {
		if len(set) == 0 {
			return nil, nil, true
		}
		where = append(where, characterTraitExistsSQL)
		args = append(args, set, model.SpoilerNone)
	}
	return where, args, false
}

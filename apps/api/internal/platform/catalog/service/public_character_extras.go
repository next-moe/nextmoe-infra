package service

import (
	"context"
	"strconv"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

func (s *PublicService) CharacterWorkCounts(ctx context.Context, ids []int64, nsfw bool) (map[int64]int, error) {
	out := make(map[int64]int, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	union := `SELECT wc.character_id, wc.work_id FROM catalog_work_character wc
		WHERE wc.character_id IN ? AND ` + editspec.NotSuppressedRosterSQL("wc") + ` AND ` + liveWorkSQL("wc.work_id") + `
		UNION SELECT c.character_id, c.work_id FROM catalog_credit c
		WHERE c.character_id IN ? AND ` + editspec.NotSuppressedCreditSQL("c") + ` AND ` + liveWorkSQL("c.work_id")
	var rows []struct {
		CharacterID int64 `gorm:"column:character_id"`
		N           int   `gorm:"column:n"`
	}
	q := `SELECT u.character_id, count(*)::int AS n FROM (` + union + `) u
		WHERE ? OR NOT EXISTS (
			SELECT 1 FROM catalog_work w WHERE w.id = u.work_id AND w.content_rating = ?
		)
		GROUP BY u.character_id`
	if err := s.db.WithContext(ctx).Raw(q, ids, ids, nsfw, model.ContentRatingR18).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.CharacterID] = r.N
	}
	return out, nil
}

func (s *PublicService) attachCharacterWorkCounts(ctx context.Context, rows []EntityListRow, nsfw bool) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	counts, err := s.CharacterWorkCounts(ctx, ids, nsfw)
	if err != nil {
		return err
	}
	for i := range rows {
		n := counts[rows[i].ID]
		rows[i].WorkCount = &n
	}
	return nil
}

func (s *PublicService) attachMatchedTraitIDs(ctx context.Context, rows []EntityListRow, sets [][]int64, nsfw bool) error {
	if len(rows) == 0 {
		return nil
	}
	union := make([]int64, 0)
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
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		empty := []string{}
		rows[i].MatchedTraitIDs = &empty
	}
	if len(union) == 0 {
		return nil
	}
	var found []struct {
		CharacterID int64 `gorm:"column:character_id"`
		TraitID     int64 `gorm:"column:trait_id"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT l.character_id, l.trait_id
		FROM catalog_character_trait_link l
		JOIN catalog_character_trait t ON t.id = l.trait_id
		WHERE l.character_id IN ? AND l.spoiler_level <= ? AND l.trait_id IN ?
		  AND (? OR NOT t.sexual_family)
		ORDER BY l.character_id, l.trait_id`,
		ids, model.SpoilerNone, union, nsfw).Scan(&found).Error; err != nil {
		return err
	}
	byChar := map[int64][]string{}
	for _, r := range found {
		byChar[r.CharacterID] = append(byChar[r.CharacterID], strconv.FormatInt(r.TraitID, 10))
	}
	for i := range rows {
		if ids := byChar[rows[i].ID]; ids != nil {
			cp := ids
			rows[i].MatchedTraitIDs = &cp
		}
	}
	return nil
}

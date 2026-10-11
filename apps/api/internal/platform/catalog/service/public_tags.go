package service

import (
	"context"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

func (s *PublicService) TagDetail(ctx context.Context, id int64, withWorks, nsfw bool, limit, offset int) (dto.PublicTagDetail, bool, error) {
	var head struct {
		ID   int64
		Name string
		Tier int16
		Kind int16
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id, name, tier, kind FROM catalog_tag WHERE id = ?`, id).Scan(&head).Error; err != nil {
		return dto.PublicTagDetail{}, false, err
	}
	if head.ID == 0 {
		return dto.PublicTagDetail{}, false, nil
	}
	rec := dto.PublicTagDetail{
		ID: head.ID, Name: head.Name, Tier: tagTierKey(head.Tier), Kind: tagKindKey(head.Kind),
	}
	counts, err := s.workCountsFor(ctx, tagWorkEdge, []int64{id}, nsfw)
	if err != nil {
		return dto.PublicTagDetail{}, false, err
	}
	rec.WorkCount = counts[id]
	sexual, err := s.tagSexualFor(ctx, []int64{id})
	if err != nil {
		return dto.PublicTagDetail{}, false, err
	}
	rec.Sexual = sexual[id]
	intros, err := s.tagIntros(ctx, id)
	if err != nil {
		return dto.PublicTagDetail{}, false, err
	}
	rec.Intros = intros
	h, err := s.tagHierarchyFor(ctx, []int64{id})
	if err != nil {
		return dto.PublicTagDetail{}, false, err
	}
	rec.Parents, rec.ChildCount, rec.DescendantCount = h.parentsOf(id), h.children[id], h.descendants[id]
	if rec.Children, err = s.tagChildren(ctx, id); err != nil {
		return dto.PublicTagDetail{}, false, err
	}
	if withWorks {
		var wrows []struct {
			WorkID int64 `gorm:"column:work_id"`
		}
		if err := s.db.WithContext(ctx).Raw(`
			SELECT DISTINCT wt.work_id FROM catalog_work_tag wt
			JOIN catalog_tag_source_map m ON m.source_id = wt.source_id AND m.source_name = wt.name
			JOIN catalog_work w ON w.id = wt.work_id AND w.deleted_at IS NULL AND w.status = ? AND w.medium_id = ?
			WHERE m.tag_id = ?
			ORDER BY wt.work_id
			LIMIT ? OFFSET ?`,
			model.WorkStatusLive, galgameMediumID, id, limit, offset).Scan(&wrows).Error; err != nil {
			return dto.PublicTagDetail{}, false, err
		}
		ids := make([]int64, len(wrows))
		for i, r := range wrows {
			ids[i] = r.WorkID
		}
		briefs, err := s.loadWorkBriefs(ctx, ids, nsfw)
		if err != nil {
			return dto.PublicTagDetail{}, false, err
		}
		rec.Works = make([]dto.PublicWorkBrief, 0, len(ids))
		for _, wid := range ids {
			if b := briefs[wid]; b != nil {
				rec.Works = append(rec.Works, *b)
			}
		}
		rec.NextOffset = nextOffset(len(wrows), limit, offset)
	}
	return rec, true, nil
}

func (s *PublicService) tagIntros(ctx context.Context, tagID int64) ([]dto.PublicIntro, error) {
	var rows []struct {
		Lang     string `gorm:"column:lang"`
		Intro    string `gorm:"column:intro"`
		SourceID int16  `gorm:"column:source_id"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT lang, intro, source_id FROM catalog_tag_intro
		WHERE tag_id = ? ORDER BY lang, `+
		editspec.HumanLaneFirstNoProvenanceSQL("source_id")+
		`, source_id`, tagID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]dto.PublicIntro, 0, len(rows))
	seenLang := map[string]bool{}
	for _, r := range rows {
		if seenLang[r.Lang] {
			continue
		}
		seenLang[r.Lang] = true
		out = append(out, dto.PublicIntro{Lang: r.Lang, Intro: r.Intro, Source: s.sourceKey(r.SourceID)})
	}
	return out, nil
}

func tagTierKey(t int16) string {
	switch t {
	case model.TagTierLongtail:
		return "longtail"
	case model.TagTierHidden:
		return "hidden"
	default:
		return "core"
	}
}

func tagKindKey(k int16) string {
	if k == model.TagKindMeta {
		return "meta"
	}
	return "content"
}

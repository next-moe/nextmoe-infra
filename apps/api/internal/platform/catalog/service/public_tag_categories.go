package service

import (
	"context"

	"api/internal/platform/catalog/dto"
)

type tagCategoryIndex map[int16]dto.PublicTagCategoryRef

func (ix tagCategoryIndex) ref(id *int16) *dto.PublicTagCategoryRef {
	if id == nil {
		return nil
	}
	if r, ok := ix[*id]; ok {
		return &r
	}
	return nil
}

func (s *PublicService) tagCategoriesByID(ctx context.Context) (tagCategoryIndex, error) {
	var rows []struct {
		ID   int16
		Slug string
		Name string
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT id, slug, name FROM catalog_tag_category`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(tagCategoryIndex, len(rows))
	for _, r := range rows {
		out[r.ID] = dto.PublicTagCategoryRef{Slug: r.Slug, Name: r.Name}
	}
	return out, nil
}

func (s *PublicService) TagCategories(ctx context.Context) ([]dto.PublicTagCategory, error) {
	out := []dto.PublicTagCategory{}
	if err := s.db.WithContext(ctx).Raw(`SELECT c.slug, c.name, c.sort_order,
			(SELECT count(*) FROM catalog_tag t WHERE t.category_id = c.id) AS tag_count
		FROM catalog_tag_category c ORDER BY c.sort_order, c.id`).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PublicService) TagCategoryIDs(ctx context.Context) (map[string]int16, error) {
	var rows []struct {
		ID   int16
		Slug string
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT id, slug FROM catalog_tag_category ORDER BY sort_order, id`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int16, len(rows))
	for _, r := range rows {
		out[r.Slug] = r.ID
	}
	return out, nil
}

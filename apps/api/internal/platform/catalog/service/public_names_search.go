package service

import (
	"context"
	"strconv"

	catsearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/search/spec"
)

const releaseTitleWorkLimit = 200

type NameSearchPage struct {
	Items []EntityListRow
	Total int64
	Page  int
	Limit int
}

func (s *PublicService) SearchAvailable() bool { return s.worksSearch != nil }

func (s *PublicService) NamesSearch(ctx context.Context, q string, page, limit int) (NameSearchPage, error) {
	if s.worksSearch == nil {
		return NameSearchPage{}, ErrSearchUnavailable
	}
	page, limit = max(page, 1), clampBrowseLimit(limit)
	res, err := s.worksSearch.SearchEntities(ctx, catsearch.IndexCreditNames, spec.EntityQuery{Q: q, Page: page, Limit: limit})
	if err != nil {
		return NameSearchPage{}, err
	}
	ids := make([]int64, 0, len(res.Hits))
	for _, h := range res.Hits {
		if len(h.ID) > 1 {
			if id, err := strconv.ParseInt(h.ID[1:], 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
	}
	items, err := s.hydrateInOrder(ids, func(ids []int64) (EntityListPage, error) {
		return s.NamesList(ctx, ids, "", "", len(ids))
	})
	return NameSearchPage{Items: items, Total: res.Total, Page: page, Limit: limit}, err
}

func (s *PublicService) PersonsSearch(ctx context.Context, q string, page, limit int) (NameSearchPage, error) {
	if s.worksSearch == nil {
		return NameSearchPage{}, ErrSearchUnavailable
	}
	page, limit = max(page, 1), clampBrowseLimit(limit)
	res, err := s.worksSearch.SearchPersons(ctx, spec.EntityQuery{Q: q, Page: page, Limit: limit})
	if err != nil {
		return NameSearchPage{}, err
	}
	items, err := s.hydrateInOrder(res.IDs, func(ids []int64) (EntityListPage, error) {
		return s.PersonsList(ctx, ids, "", "", len(ids))
	})
	return NameSearchPage{Items: items, Total: res.Total, Page: page, Limit: limit}, err
}

func (s *PublicService) WorkIDsMatchingTitle(ctx context.Context, q string) ([]int64, error) {
	if s.worksSearch == nil || q == "" {
		return nil, nil
	}
	res, err := s.worksSearch.SearchWorks(ctx, spec.WorksQuery{
		Q: q, Page: 1, Limit: releaseTitleWorkLimit, Filter: spec.WorksFilter{OLang: spec.OLang{All: true}},
	})
	if err != nil {
		return nil, err
	}
	return res.IDs, nil
}

func (s *PublicService) hydrateInOrder(ids []int64, load func([]int64) (EntityListPage, error)) ([]EntityListRow, error) {
	if len(ids) == 0 {
		return []EntityListRow{}, nil
	}
	page, err := load(ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]EntityListRow, len(page.Items))
	for _, row := range page.Items {
		byID[row.ID] = row
	}
	out := make([]EntityListRow, 0, len(ids))
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

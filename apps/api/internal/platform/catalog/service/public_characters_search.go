package service

import (
	"context"
	"sync"
	"time"

	"api/internal/platform/catalog/search/spec"

	"golang.org/x/sync/singleflight"
)

type CharactersSearchFilter struct {
	Q        string
	TraitIDs []int64
	MatchAny bool
	Genders  []int16
	NSFW     bool
	Sort     string
	Page     int
	Limit    int
	Include  CharacterListInclude
}

type CharactersSearchPage struct {
	Items []EntityListRow
	Total int64
	Page  int
	Limit int
}

func (s *PublicService) CharactersSearch(ctx context.Context, f CharactersSearchFilter) (CharactersSearchPage, error) {
	if s.worksSearch == nil {
		return CharactersSearchPage{}, ErrSearchUnavailable
	}
	page, limit := f.Page, f.Limit
	if page < 1 {
		page = 1
	}
	limit = clampBrowseLimit(limit)

	var traitSets [][]int64
	if len(f.TraitIDs) > 0 {
		sets, err := s.expandCharacterTraits(ctx, f.TraitIDs, f.NSFW)
		if err != nil {
			return CharactersSearchPage{}, err
		}
		traitSets = sets
	}

	res, err := s.worksSearch.SearchCharacters(ctx, spec.CharacterQuery{
		Q:             f.Q,
		Page:          page,
		Limit:         limit,
		Sort:          f.Sort,
		TraitIDs:      f.TraitIDs,
		TraitMatchAny: f.MatchAny,
		NSFW:          f.NSFW,
		Genders:       f.Genders,
	})
	if err != nil {
		return CharactersSearchPage{}, err
	}
	items, err := s.hydrateCharacterIDs(ctx, res.IDs, f.NSFW, f.Include)
	if err != nil {
		return CharactersSearchPage{}, err
	}
	if len(f.TraitIDs) > 0 {
		if err := s.attachMatchedTraitIDs(ctx, items, traitSets, f.NSFW); err != nil {
			return CharactersSearchPage{}, err
		}
	}
	return CharactersSearchPage{Items: items, Total: res.Total, Page: page, Limit: limit}, nil
}

func (s *PublicService) hydrateCharacterIDs(ctx context.Context, ids []int64, nsfw bool, inc CharacterListInclude) ([]EntityListRow, error) {
	if len(ids) == 0 {
		return []EntityListRow{}, nil
	}
	page, err := s.CharactersList(ctx, ids, "", len(ids), inc, nsfw, CharacterTraitFilter{}, false)
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

const traitCountTTL = 10 * time.Minute

type traitCountCache struct {
	mu   sync.Mutex
	sf   singleflight.Group
	sfw  *traitCountSnap
	nsfw *traitCountSnap
}

type traitCountSnap struct {
	at time.Time
	m  map[int64]int64
}

func newTraitCountCache() *traitCountCache {
	return &traitCountCache{}
}

func (c *traitCountCache) get(nsfw bool) map[int64]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	snap := c.sfw
	if nsfw {
		snap = c.nsfw
	}
	if snap == nil || time.Since(snap.at) >= traitCountTTL {
		return nil
	}
	return snap.m
}

func (c *traitCountCache) put(nsfw bool, m map[int64]int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	snap := &traitCountSnap{at: time.Now(), m: m}
	if nsfw {
		c.nsfw = snap
	} else {
		c.sfw = snap
	}
}

func (s *PublicService) CharacterTraitCounts(ctx context.Context, nsfw bool) (map[int64]int64, error) {
	if s.worksSearch == nil {
		return nil, ErrSearchUnavailable
	}
	if s.traitCounts == nil {
		s.traitCounts = newTraitCountCache()
	}
	if cached := s.traitCounts.get(nsfw); cached != nil {
		return cached, nil
	}
	key := "sfw"
	if nsfw {
		key = "nsfw"
	}
	v, err, _ := s.traitCounts.sf.Do(key, func() (any, error) {
		if cached := s.traitCounts.get(nsfw); cached != nil {
			return cached, nil
		}
		m, err := s.worksSearch.CharacterTraitCounts(ctx, nsfw)
		if err != nil {
			return nil, err
		}
		if m == nil {
			m = map[int64]int64{}
		}
		s.traitCounts.put(nsfw, m)
		return m, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(map[int64]int64), nil
}

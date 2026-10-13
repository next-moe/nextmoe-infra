package service

import (
	"context"
	stderrors "errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
	catsearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/search/spec"
)

var ErrSearchUnavailable = stderrors.New("catalog: works search indexer not configured")

func (s *PublicService) WithWorksSearch(idx *catsearch.Indexer) *PublicService {
	s.worksSearch = idx
	return s
}

type WorksSearchFilter struct {
	Q              string
	ContentRating  *int16
	Claimed        *bool
	ClaimStates    []string
	DisplayLimits  []string
	LabelID        int64
	TagIDs         []int64
	ExcludeTagIDs  []int64
	EngineID       int64
	SeriesID       int64
	ReleasedAfter  int64
	ReleasedBefore int64
	OLang          PublicOLang
	NSFW           bool
	Sort           string
	Facets         []string
	Page           int
	Limit          int
	Include        WorksListInclude
	Fields         PublicFields
	SearchIntro    bool
}

var worksSearchFacetAttr = map[string]string{
	"content_rating": "content_rating",
	"olang":          "olang",
	"claimed":        "claimed",
	"tag_id":         "tag_ids",
	"label_id":       "label_ids",
	"company_id":     "label_ids",
	"engine_id":      "engine_ids",
	"series_id":      "series_ids",
	"source":         "source_keys",
}

var WorksSearchFacetTokens = []string{
	"content_rating", "olang", "claimed", "tag_id", "label_id", "engine_id", "series_id", "source",
}

func IsWorksSearchFacet(tok string) bool {
	_, ok := worksSearchFacetAttr[tok]
	return ok
}

var worksSearchSortRules = map[string]string{
	"":              "",
	"relevance":     "",
	"released_desc": "released_ord:desc",
	"released_asc":  "released_ord:asc",
	"updated":       "updated_ts:desc",
	"popularity":    "popularity:desc",

	"rating_bangumi":      "rating.bangumi:desc",
	"rating_vndb":         "rating.vndb:desc",
	"rating_erogamescape": "rating.erogamescape:desc",
	"rating_dlsite":       "rating.dlsite:desc",
	"rating_nextmoe":      "rating.nextmoe:desc",
}

var WorksSearchSortTokens = []string{
	"relevance", "released_desc", "released_asc", "updated", "popularity",
	"rating_bangumi", "rating_vndb", "rating_erogamescape", "rating_dlsite", "rating_nextmoe",
}

var WorksSearchClaimStateTokens = []string{
	model.ClaimStateKeyNone, model.ClaimStateKeyLive,
	model.ClaimStateKeyDraft, model.ClaimStateKeyPending,
	model.ClaimStateKeyDeclined, model.ClaimStateKeyHidden,
}

func IsWorksSearchClaimState(tok string) bool {
	for _, v := range WorksSearchClaimStateTokens {
		if tok == v {
			return true
		}
	}
	return false
}

func WorksSearchSortRule(sort string) (rule string, ok bool) {
	rule, ok = worksSearchSortRules[strings.TrimSpace(sort)]
	return rule, ok
}

func (s *PublicService) WorksSearch(ctx context.Context, f WorksSearchFilter) (dto.PublicWorksSearchData, error) {
	if s.worksSearch == nil {
		return dto.PublicWorksSearchData{}, ErrSearchUnavailable
	}
	page, limit := f.Page, f.Limit
	if page < 1 {
		page = 1
	}
	limit = clampBrowseLimit(limit)

	text := f.Q
	docID := ""
	if vid := normalizeVNDBID(f.Q); vid != "" {
		workID, err := s.lookupEntityID(ctx, "vndb", vid, model.EntityTypeWork)
		if err != nil {
			return dto.PublicWorksSearchData{}, err
		}
		text, docID = "", catsearch.WorkDocID(workID)
	}

	rule, _ := WorksSearchSortRule(f.Sort)
	res, err := s.worksSearch.SearchWorks(ctx, spec.WorksQuery{
		Q:           text,
		Filter:      f.worksFilter(docID),
		SortLane:    rule,
		FacetAttrs:  worksSearchFacetAttrs(f.Facets),
		Page:        page,
		Limit:       limit,
		SearchIntro: f.SearchIntro,
	})
	if err != nil {
		return dto.PublicWorksSearchData{}, err
	}

	items, err := s.hydrateWorkIDs(ctx, res.IDs, f.NSFW, f.Include, f.Fields)
	if err != nil {
		return dto.PublicWorksSearchData{}, err
	}
	return dto.PublicWorksSearchData{
		Total: res.Total, Page: page, Limit: limit, Items: items,
		Facets: projectWorksSearchFacets(f.Facets, res.Facets),
	}, nil
}

func (f WorksSearchFilter) worksFilter(docID string) spec.WorksFilter {
	wf := spec.WorksFilter{
		DocID:          docID,
		ContentRating:  f.ContentRating,
		Claimed:        f.Claimed,
		ClaimStates:    f.ClaimStates,
		DisplayLimits:  f.DisplayLimits,
		TagIDs:         f.TagIDs,
		ExcludeTagIDs:  f.ExcludeTagIDs,
		LabelID:        f.LabelID,
		EngineID:       f.EngineID,
		SeriesID:       f.SeriesID,
		ReleasedAfter:  f.ReleasedAfter,
		ReleasedBefore: f.ReleasedBefore,
		OLang:          f.OLang.Spec(),
	}
	if !f.NSFW {
		r := model.ContentRatingR18
		wf.ContentRatingNot = &r
	}
	// The browse lane's ban exclusion: a banned work must not come
	// back through q= either, and it did while claim_state= was absent.
	if !slices.Contains(f.ClaimStates, model.ClaimStateKeyHidden) {
		wf.ClaimStateNot = model.ClaimStateKeyHidden
	}
	return wf
}

func worksSearchFacetAttrs(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	out := make([]string, 0, len(tokens))
	seen := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		attr, ok := worksSearchFacetAttr[t]
		if !ok || seen[attr] {
			continue
		}
		seen[attr] = true
		out = append(out, attr)
	}
	return out
}

func projectWorksSearchFacets(tokens []string, dist map[string]map[string]int64) map[string]map[string]int64 {
	if len(tokens) == 0 || len(dist) == 0 {
		return nil
	}
	out := make(map[string]map[string]int64, len(tokens))
	for _, t := range tokens {
		attr, ok := worksSearchFacetAttr[t]
		if !ok {
			continue
		}
		values, ok := dist[attr]
		if !ok {
			continue
		}
		bucket := make(map[string]int64, len(values))
		for k, n := range values {
			if t == "content_rating" {
				if rating, err := strconv.Atoi(k); err == nil {
					if key := contentRatingKey(int16(rating)); key != "" {
						bucket[key] = n
						continue
					}
				}
			}
			bucket[k] = n
		}
		out[t] = bucket
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeVNDBID(q string) string {
	q = strings.TrimSpace(q)
	if len(q) < 2 || (q[0] != 'v' && q[0] != 'V') {
		return ""
	}
	for i := 1; i < len(q); i++ {
		if q[i] < '0' || q[i] > '9' {
			return ""
		}
	}
	return strings.ToLower(q)
}

func (s *PublicService) hydrateWorkIDs(ctx context.Context, ids []int64, nsfw bool, inc WorksListInclude, sel PublicFields) ([]dto.PublicWorkListItem, error) {
	if len(ids) == 0 {
		return []dto.PublicWorkListItem{}, nil
	}
	where := []string{"w.deleted_at IS NULL", "w.status = ?", "w.medium_id = ?", "w.id IN ?"}
	args := []any{model.WorkStatusLive, galgameMediumID, ids}
	if !nsfw {
		where = append(where, "w.content_rating <> ?")
		args = append(args, model.ContentRatingR18)
	}
	var rows []struct {
		ID          int64
		MediumID    int16
		DisplayName string
		// Explicit column tag: GORM snake-cases the field to o_lang, which
		// matches no result column (the W1 works-list trap, fixed in 0dce1f36).
		OLang         string `gorm:"column:olang"`
		ContentRating int16
		Site          *string
		ProductWorkID *int64
		ClaimState    *int16 `gorm:"column:claim_state"`
		CreatedAt     time.Time
		UpdatedAt     time.Time
	}
	q := `SELECT w.id, w.medium_id, w.display_name, w.olang, w.content_rating, w.site, w.product_work_id, w.claim_state, w.created_at, w.updated_at
		FROM catalog_work w WHERE ` + strings.Join(where, " AND ")
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	byID := make(map[int64]workListSourceRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = workListSourceRow{
			ID: r.ID, MediumID: r.MediumID, DisplayName: r.DisplayName, OLang: r.OLang,
			ContentRating: r.ContentRating, Site: r.Site, ProductWorkID: r.ProductWorkID,
			ClaimState: r.ClaimState, CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339),
		}
	}
	src := make([]workListSourceRow, 0, len(ids))
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			src = append(src, row)
		}
	}
	return s.enrichWorkListItems(ctx, src, nsfw, inc, sel)
}

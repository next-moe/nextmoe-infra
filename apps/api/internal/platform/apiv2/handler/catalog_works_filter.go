package handler

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/parse"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/dto"
	catmodel "api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

type worksFilter struct {
	Q              string
	ContentRating  *int16
	Claimed        *bool
	ClaimStates    []string
	DisplayLimits  []string
	Site           string
	OwnerUID       int64
	CompanyID      int64
	CompanyRollup  bool
	TagIDs         []int64
	SeriesID       int64
	EngineID       int64
	Platform       string
	ReleasedAfter  int64
	ReleasedBefore int64
	OLang          catsvc.PublicOLang
}

var publicClaimStates = []string{
	catmodel.ClaimStateKeyNone, catmodel.ClaimStateKeyLive, catmodel.ClaimStateKeyDraft,
}

var moderationClaimStates = []string{
	catmodel.ClaimStateKeyPending, catmodel.ClaimStateKeyDeclined, catmodel.ClaimStateKeyHidden,
}

func mentionsModerationClaimState(raw string) bool {
	for _, tok := range strings.Split(raw, ",") {
		if slices.Contains(moderationClaimStates, strings.TrimSpace(tok)) {
			return true
		}
	}
	return false
}

// moderationClaimStateSite answers the caller's own site when it may read the
// moderation states, "" when it may not — in which case parseWorksFilter keeps
// the public three-state vocabulary and the request gets the identical closed
// -enum 400 it got before, so the wider set stays invisible.
func (c *Catalog) moderationClaimStateSite(ctx context.Context, rawClaimState string) (string, *problem.Problem) {
	if !mentionsModerationClaimState(rawClaimState) {
		return "", nil
	}
	if !catalogAuthzFrom(ctx).ModerationRead {
		return "", nil
	}
	if c == nil || c.SiteOfAppClient == nil {
		return "", problem.New(problem.CodeServiceUnavailable, "", "", "the moderation site resolver is not bound.")
	}
	site, err := c.SiteOfAppClient(ctx, catalogAuthzFrom(ctx).ClientID)
	if err != nil {
		return "", problem.New(problem.CodeServiceUnavailable, "", "", "the moderation site resolver is unavailable.")
	}
	if site == "" {
		return "", problem.New(problem.CodeSiteNotBound, "", "",
			"this key's application is not bound to a catalog site, so it has no moderation queue.")
	}
	return site, nil
}

func fenceModerationSite(f worksFilter, ownSite string) *problem.Problem {
	moderation := false
	for _, st := range f.ClaimStates {
		if slices.Contains(moderationClaimStates, st) {
			moderation = true
			break
		}
	}
	if !moderation {
		return nil
	}
	if f.Site == "" {
		p := problem.New(problem.CodeValidationFailed, "", "", "a moderation claim_state needs site= to be unambiguous.")
		p.Errors = []problem.FieldError{{Parameter: "site", Reason: problem.ReasonRequired,
			Detail: "moderation states are per-site queues; pass site=" + ownSite}}
		return p
	}
	if f.Site != ownSite {
		return problem.New(problem.CodePermissionRequired, "", "",
			"a moderation claim_state is fenced to the caller's own site.")
	}
	return nil
}

func parseWorksFilter(in *listWorksInput, moderationAllowed bool) (worksFilter, *problem.Problem) {
	f := worksFilter{OLang: catsvc.PublicOLang{All: true}}
	if in == nil {
		return f, nil
	}
	f.Q = strings.TrimSpace(in.Q)
	if in.ContentRating != "" {
		v, ok := contentRatingFromKey(in.ContentRating)
		if !ok {
			return worksFilter{}, closedParam("content_rating", "all_ages, sensitive, r18")
		}
		f.ContentRating = &v
	}
	if in.Claimed != "" {
		v, err := parse.Bool(in.Claimed, "claimed")
		if err != nil {
			return worksFilter{}, err
		}
		f.Claimed = &v
	}
	// R1 closed this to the public three and pointed the queue at
	// /v2/moderation/claims, but the forum's admin queue reads it from here with
	// an application key, which that face does not accept — the review queue
	// answered 400 in production and rendered as "no pending submissions".
	allowedStates := publicClaimStates
	if moderationAllowed {
		allowedStates = append(append([]string{}, publicClaimStates...), moderationClaimStates...)
	}
	states, err := closedCSV(in.ClaimState, "claim_state", allowedStates)
	if err != nil {
		return worksFilter{}, err
	}
	f.ClaimStates = states
	limits, err := closedCSV(in.ContentLimit, "content_limit", []string{
		catmodel.DisplayLimitKeySFW, catmodel.DisplayLimitKeyNSFW,
	})
	if err != nil {
		return worksFilter{}, err
	}
	f.DisplayLimits = limits
	f.Site = strings.TrimSpace(in.Site)
	owner, err := optionalID(in.OwnerUID, "owner_uid")
	if err != nil {
		return worksFilter{}, err
	}
	f.OwnerUID = owner
	if f.OwnerUID > 0 && f.Site == "" {
		p := problem.New(problem.CodeValidationFailed, "", "", "owner_uid= needs site= to be unambiguous.")
		p.Errors = []problem.FieldError{{Parameter: "owner_uid", Reason: problem.ReasonInconsistentWith,
			Detail: "owner uids are the claiming site's own user ids; pass site= as well"}}
		return worksFilter{}, p
	}
	id, err := optionalID(in.CompanyID, "company_id")
	if err != nil {
		return worksFilter{}, err
	}
	f.CompanyID = id
	if in.CompanyRollup != "" {
		v, berr := parse.Bool(in.CompanyRollup, "company_rollup")
		if berr != nil {
			return worksFilter{}, berr
		}
		f.CompanyRollup = v
	}
	tags, err := idList(in.TagID, "tag_id", 10)
	if err != nil {
		return worksFilter{}, err
	}
	f.TagIDs = tags
	if f.SeriesID, err = optionalID(in.SeriesID, "series_id"); err != nil {
		return worksFilter{}, err
	}
	if f.EngineID, err = optionalID(in.EngineID, "engine_id"); err != nil {
		return worksFilter{}, err
	}
	f.Platform = strings.TrimSpace(in.Platform)
	if f.ReleasedAfter, err = dateOrdinal(in.ReleasedAfter, "released_after"); err != nil {
		return worksFilter{}, err
	}
	if f.ReleasedBefore, err = dateOrdinal(in.ReleasedBefore, "released_before"); err != nil {
		return worksFilter{}, err
	}
	olang, err := parseOLang(in.OLang)
	if err != nil {
		return worksFilter{}, err
	}
	f.OLang = olang
	return f, nil
}

func (c *Catalog) ListWorksFiltered(ctx context.Context, q collect.Query, f worksFilter) (repr.List[repr.Work], error) {
	if c == nil || c.Public == nil {
		return repr.List[repr.Work]{}, problem.New(problem.CodeServiceUnavailable, "", "", "works collection is not bound.")
	}
	if f.ContentRating != nil && *f.ContentRating == catmodel.ContentRatingR18 && !q.NSFW {
		p := problem.New(problem.CodeInvalidParameter, "", "", "content_rating=r18 requires nsfw=true.")
		p.Errors = []problem.FieldError{{Parameter: "content_rating", Reason: problem.ReasonNotAllowedValue, Detail: "nsfw=true is required"}}
		return repr.List[repr.Work]{}, p
	}
	if q.Sort == "relevance" && f.Q == "" && !q.Batch {
		p := problem.New(problem.CodeInvalidParameter, "", "", "sort=relevance requires q=.")
		p.Errors = []problem.FieldError{{Parameter: "sort", Reason: problem.ReasonNotAllowedValue, Detail: "pass q="}}
		return repr.List[repr.Work]{}, p
	}
	if q.Batch && f.Q != "" {
		p := problem.New(problem.CodeMutuallyExclusiveParameters, "", "", "q= cannot be combined with ids= or refs=.")
		p.Errors = []problem.FieldError{{Parameter: "q", Reason: problem.ReasonNotAllowedValue, Detail: "ids=/refs= is a batch lane"}}
		return repr.List[repr.Work]{}, p
	}
	inc := listWorksInclude(q.Include)
	if q.Batch || (!searchWorksRequested(q, f)) {
		return c.listWorksSQL(ctx, q, f, inc)
	}
	if f.OwnerUID > 0 {
		p := problem.New(problem.CodeMutuallyExclusiveParameters, "", "", "owner_uid= cannot be combined with q=, page= or search sorts.")
		p.Errors = []problem.FieldError{{Parameter: "owner_uid", Reason: problem.ReasonNotAllowedValue, Detail: "the search index carries no claim owner"}}
		return repr.List[repr.Work]{}, p
	}
	// WorksSearchFilter has no Site and no Platform member, so these two were
	// accepted, dropped on the way to the search engine and answered with the
	// unfiltered population — including whenever facets= alone switched the
	// collection to search, which is how the consumers reach this lane. They are
	// refused rather than filtered because neither is a filterable attribute on
	// the works index; making them work is a document-shape change and a
	// reindex, not a parameter fix.
	if f.Site != "" || f.Platform != "" {
		name := "site"
		if f.Site == "" {
			name = "platform"
		}
		p := problem.New(problem.CodeMutuallyExclusiveParameters, "", "", name+"= cannot be combined with q=, facets=, page= or a search sort.")
		p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonNotAllowedValue,
			Detail: "the search index carries neither the claiming site nor the platform; drop q=/facets=/page=/the search sort to filter on it"}}
		return repr.List[repr.Work]{}, p
	}
	return c.listWorksSearch(ctx, q, f, inc)
}

func searchWorksRequested(q collect.Query, f worksFilter) bool {
	if f.Q != "" || collect.SearchSort(q.Sort) || q.Page > 0 {
		return true
	}
	return len(q.Facets) > 0
}

func (c *Catalog) listWorksSQL(ctx context.Context, q collect.Query, f worksFilter, inc catsvc.WorksListInclude) (repr.List[repr.Work], error) {
	lf := catsvc.WorksListFilter{
		ContentRating: f.ContentRating, Claimed: f.Claimed, ClaimStates: f.ClaimStates,
		DisplayLimits: f.DisplayLimits, Site: f.Site, OwnerUID: f.OwnerUID,
		LabelID: f.CompanyID, LabelRollup: f.CompanyRollup,
		TagIDs: f.TagIDs, SeriesID: f.SeriesID, EngineID: f.EngineID, Platform: f.Platform,
		ReleasedAfter: f.ReleasedAfter, ReleasedBefore: f.ReleasedBefore, NSFW: q.NSFW,
		Sort: q.Sort, OLang: f.OLang, Include: inc, IncludeTotal: q.IncludeTotal,
	}
	if lf.Sort == "" || collect.SearchSort(lf.Sort) {
		lf.Sort = "id"
	}
	var missing []string
	if q.Batch {
		ids, miss, err := c.batchWorkIDs(ctx, q)
		if err != nil {
			return repr.List[repr.Work]{}, err
		}
		lf.IDs = ids
		missing = miss
		q.Cursor = ""
	}
	// Every entity list guards the all-miss batch; works shipped without it, so
	// refs= that resolved nothing dropped the IDs filter and answered an
	// unfiltered page 1 of the whole catalogue (found by the forum's client).
	if q.Batch && len(lf.IDs) == 0 {
		out := finishList([]repr.Work{}, nil, 0, q, missing)
		if len(q.Facets) > 0 {
			out.Facets = emptyFacets(q.Facets)
		}
		return out, nil
	}
	limit := q.Limit
	if q.Batch {
		limit = 100
	}
	data, err := c.Public.WorksList(ctx, lf, q.Cursor, limit)
	if err != nil {
		if errors.Is(err, catsvc.ErrBadCursor) {
			return repr.List[repr.Work]{}, collectInvalidCursor()
		}
		return repr.List[repr.Work]{}, err
	}
	items := make([]repr.Work, 0, len(data.Items))
	seen := map[int64]bool{}
	for _, it := range data.Items {
		items = append(items, workFromListItem(it, q.Include, c.imageURL))
		seen[it.ID] = true
	}
	if q.Batch {
		for _, id := range lf.IDs {
			if !seen[id] {
				missing = append(missing, repr.ID(id))
			}
		}
	}
	out := finishList(items, data.NextCursor, data.Total, q, missing)
	if len(q.Facets) > 0 {
		out.Facets = emptyFacets(q.Facets)
	}
	return out, nil
}

func (c *Catalog) listWorksSearch(ctx context.Context, q collect.Query, f worksFilter, inc catsvc.WorksListInclude) (repr.List[repr.Work], error) {
	page, perr := searchPage(q)
	if perr != nil {
		return repr.List[repr.Work]{}, perr
	}
	sort := q.Sort
	if f.Q != "" && (sort == "" || sort == "id") {
		sort = "relevance"
	}
	sf := catsvc.WorksSearchFilter{
		Q: f.Q, ContentRating: f.ContentRating, Claimed: f.Claimed, ClaimStates: f.ClaimStates,
		DisplayLimits: f.DisplayLimits, LabelID: f.CompanyID, TagIDs: f.TagIDs, EngineID: f.EngineID,
		SeriesID: f.SeriesID, ReleasedAfter: f.ReleasedAfter, ReleasedBefore: f.ReleasedBefore,
		OLang: f.OLang, NSFW: q.NSFW, Sort: sort, Facets: searchFacetTokens(q.Facets),
		Page: page, Limit: q.Limit, Include: inc,
	}
	data, err := searchWorks(c, ctx, sf)
	if err != nil {
		if errors.Is(err, catsvc.ErrSearchUnavailable) {
			return repr.List[repr.Work]{}, problem.New(problem.CodeServiceUnavailable, "", "", "works search is not bound.")
		}
		return repr.List[repr.Work]{}, err
	}
	items := make([]repr.Work, 0, len(data.Items))
	for _, it := range data.Items {
		items = append(items, workFromListItem(it, q.Include, c.imageURL))
	}
	if q.Page > 0 {
		out := finishPageList(items, data.Total, q, nil)
		if len(q.Facets) > 0 {
			out.Facets = mapSearchFacets(q.Facets, data.Facets)
		}
		return out, nil
	}
	var next *string
	if int64(page)*int64(data.Limit) < data.Total && len(data.Items) > 0 {
		enc := collect.EncodeCursor(strconv.Itoa(page + 1))
		next = &enc
	}
	out := repr.NewList(items, next)
	if q.IncludeTotal {
		n := data.Total
		out.Total = &n
	}
	if len(q.Facets) > 0 {
		out.Facets = mapSearchFacets(q.Facets, data.Facets)
	}
	return out, nil
}

var searchWorks = func(c *Catalog, ctx context.Context, sf catsvc.WorksSearchFilter) (dto.PublicWorksSearchData, error) {
	return c.Public.WorksSearch(ctx, sf)
}

func listWorksInclude(tokens []string) catsvc.WorksListInclude {
	var inc catsvc.WorksListInclude
	for _, t := range tokens {
		switch t {
		case "titles":
			inc.Names = true
		case "intros":
			inc.Intros = true
		case "companies":
			inc.Labels = true
		case "ratings":
			inc.Ratings = true
		case "covers":
			inc.Covers = true
		case "refs":
			inc.Refs = true
		case "tags":
			inc.Tags = true
		case "credits":
			inc.Credits = true
		}
	}
	return inc
}

func searchFacetTokens(in []string) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		switch t {
		case "company_id":
			out = append(out, "company_id")
		case "tag_id", "olang", "content_rating":
			out = append(out, t)
		}
	}
	return out
}

func mapSearchFacets(want []string, dist map[string]map[string]int64) *map[string][]repr.FacetValue {
	out := map[string][]repr.FacetValue{}
	for _, name := range want {
		srcName := name
		if name == "company_id" {
			if _, ok := dist["company_id"]; !ok {
				srcName = "label_id"
			}
		}
		bucket := dist[srcName]
		vals := make([]repr.FacetValue, 0, len(bucket))
		for k, n := range bucket {
			vals = append(vals, repr.FacetValue{Value: k, DisplayName: k, Count: int(n)})
		}
		if vals == nil {
			vals = []repr.FacetValue{}
		}
		out[name] = vals
	}
	return &out
}

func emptyFacets(want []string) *map[string][]repr.FacetValue {
	out := map[string][]repr.FacetValue{}
	for _, name := range want {
		out[name] = []repr.FacetValue{}
	}
	return &out
}

func contentRatingFromKey(s string) (int16, bool) {
	switch s {
	case "all_ages":
		return catmodel.ContentRatingAllAges, true
	case "sensitive":
		return catmodel.ContentRatingSensitive, true
	case "r18":
		return catmodel.ContentRatingR18, true
	default:
		return 0, false
	}
}

func closedParam(name, allowed string) *problem.Problem {
	vals := strings.Split(allowed, ", ")
	p := problem.New(problem.CodeUnknownEnumValue, "", "", name+" is not in the closed vocabulary.")
	p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonUnknownValue, Detail: "allowed values: " + allowed,
		Params: &problem.FieldParams{Allowed: &vals}}}
	return p
}

func closedCSV(raw, name string, allowed []string) ([]string, *problem.Problem) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	allow := map[string]bool{}
	for _, a := range allowed {
		allow[a] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if !allow[tok] {
			return nil, closedParam(name, strings.Join(allowed, ", "))
		}
		if seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out, nil
}

func optionalID(raw, name string) (int64, *problem.Problem) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, ok := repr.ParseID(raw)
	if !ok || n <= 0 {
		p := problem.New(problem.CodeInvalidParameter, "", "", name+" must be a decimal catalog id.")
		p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonInvalidFormat, Detail: raw}}
		return 0, p
	}
	return n, nil
}

func idList(raw, name string, max int) ([]int64, *problem.Problem) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > max {
		p := problem.New(problem.CodeInvalidParameter, "", "", name+" accepts at most "+strconv.Itoa(max)+" values.")
		p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonOutOfRange, Detail: "maximum " + strconv.Itoa(max),
			Params: &problem.FieldParams{Maximum: problem.Ptr(float64(max))}}}
		return nil, p
	}
	var out []int64
	for _, p := range parts {
		n, err := optionalID(p, name)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			out = append(out, n)
		}
	}
	return out, nil
}

func dateOrdinal(raw, name string) (int64, *problem.Problem) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	t, perr := parse.Date(raw, name)
	if perr != nil {
		return 0, perr
	}
	return int64(t.Year())*10000 + int64(t.Month())*100 + int64(t.Day()), nil
}

func parseOLang(raw string) (catsvc.PublicOLang, *problem.Problem) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return catsvc.PublicOLang{All: true}, nil
	}
	if raw == "all" {
		return catsvc.PublicOLang{All: true}, nil
	}
	var vals []string
	seen := map[string]bool{}
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" || seen[tok] {
			continue
		}
		seen[tok] = true
		vals = append(vals, tok)
	}
	return catsvc.PublicOLang{Values: vals}, nil
}

package handler

import (
	"context"
	"errors"
	"fmt"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catmodel "api/internal/platform/catalog/model"
	catsearch "api/internal/platform/catalog/search"
	catsvc "api/internal/platform/catalog/service"
	"api/internal/platform/editing"
	newsdto "api/internal/platform/news/dto"
	newssvc "api/internal/platform/news/service"
	"api/internal/platform/store/price"
	storesvc "api/internal/platform/store/service"
)

type Catalog struct {
	Public      *catsvc.PublicService
	Resolve     *catsvc.ResolveService
	StatsSvc    *catsvc.StatsService
	News        *newssvc.PublicService
	NewsWrite   *newssvc.SubmissionService
	Searcher    *catsearch.Indexer
	EditTypes   *editing.Registry
	Playtime    *catsvc.UserPlaytimeService
	WorkStates  *catsvc.UserWorkStateService
	Folders     *catsvc.UserFolderService
	CoverVotes  *catsvc.CoverVoteService
	Claims      *catsvc.ClaimLifecycleService
	Engine      *editing.Engine
	EditHistory *catsvc.EditHistoryService
	Uploads     EditImageUpload
	Store       *storesvc.Service
	Prices      *price.Service
	// oauth_clients lives in the infra database, which no catalog service can
	// reach, so the moderation fence takes its client -> site resolver here.
	SiteOfAppClient func(ctx context.Context, clientID string) (string, error)
}

func (c *Catalog) ListWorks(ctx context.Context, q collect.Query) (repr.List[repr.Work], error) {
	return c.ListWorksFiltered(ctx, q, worksFilter{OLang: catsvc.PublicOLang{All: true}})
}

func (c *Catalog) imageURL(hash string) string {
	if c == nil || c.Public == nil {
		return ""
	}
	return c.Public.ImageURL(hash)
}

func (c *Catalog) batchWorkIDs(ctx context.Context, q collect.Query) ([]int64, []string, error) {
	var ids []int64
	var missing []string
	for _, s := range q.IDs {
		n, ok := repr.ParseID(s)
		if !ok {
			p := problem.New(problem.CodeInvalidParameter, "", "", "ids= values must be decimal catalog ids.")
			p.Errors = []problem.FieldError{{Parameter: "ids", Reason: problem.ReasonInvalidFormat, Detail: s}}
			return nil, nil, p
		}
		ids = append(ids, n)
	}
	for _, r := range q.Refs {
		data, found, err := c.Public.Lookup(ctx, r.Source, r.ExternalID, q.NSFW)
		if err != nil {
			return nil, nil, err
		}
		if !found || data.Work == nil {
			missing = append(missing, r.Source+":"+r.ExternalID)
			continue
		}
		ids = append(ids, data.Work.ID)
	}
	return ids, missing, nil
}

func (c *Catalog) GetWork(ctx context.Context, id int64, nsfw bool, spoiler int16, include []string) (repr.Work, error) {
	if c == nil || c.Public == nil {
		return repr.Work{}, problem.New(problem.CodeServiceUnavailable, "", "", "works collection is not bound.")
	}
	inc := catsvc.PublicInclude{}
	for _, t := range include {
		if t == "relations" {
			inc.Relations = true
		}
		if t == "credits" {
			inc.Credits = true
		}
	}
	rec, found, err := c.Public.WorkDetail(ctx, id, inc, nsfw, spoiler, workDetailSel(include))
	if err != nil {
		return repr.Work{}, err
	}
	if found {
		return workFromDetail(rec, include, c.imageURL), nil
	}
	return repr.Work{}, c.mergedOrNotFound(ctx, catmodel.EntityTypeWork, "work", id)
}

func workDetailSel(include []string) catsvc.PublicFields {
	keys := "id,medium,display_name,latin,localized,olang,content_rating,release_date,created,updated,claimed_by,cover_slots"
	for _, t := range include {
		if t == "companies" {
			t = "labels"
		}
		keys += "," + t
	}
	return catsvc.ParsePublicFields(keys)
}

func (c *Catalog) Stats(ctx context.Context) (repr.CatalogStats, error) {
	if c == nil || c.StatsSvc == nil {
		return repr.CatalogStats{}, problem.New(problem.CodeServiceUnavailable, "", "", "stats are not bound.")
	}
	sum, err := c.StatsSvc.PublicSummary(ctx)
	if err != nil {
		return repr.CatalogStats{}, err
	}
	return repr.CatalogStats{
		Object: "catalog_stats", Works: sum.WorksTotal,
		Labels: sum.Entities.Labels, Characters: sum.Entities.Characters,
		CreditNames: sum.Entities.CreditNames, Persons: sum.Entities.Persons,
	}, nil
}

func (c *Catalog) ListNews(ctx context.Context, q collect.Query, filter newssvc.FeedFilter) (repr.List[repr.NewsItem], error) {
	if c == nil || c.News == nil {
		return repr.List[repr.NewsItem]{}, problem.New(problem.CodeServiceUnavailable, "", "", "news is not bound.")
	}
	data, err := c.News.Feed(ctx, filter, q.Cursor, q.Limit)
	if err != nil {
		if errors.Is(err, newssvc.ErrBadCursor) {
			return repr.List[repr.NewsItem]{}, collectInvalidCursor()
		}
		return repr.List[repr.NewsItem]{}, err
	}
	items := make([]repr.NewsItem, 0, len(data.Items))
	for _, it := range data.Items {
		items = append(items, newsFromDTO(it))
	}
	// Hand-rolled repr.NewList instead of finishList was why /v2/news was the
	// one face of the twenty-six declaring include_total= that never answered a
	// total; the forum's news client asks for one on every request.
	var total int64
	if q.IncludeTotal {
		n, _, _, merr := c.News.FeedMeta(ctx, filter)
		if merr != nil {
			return repr.List[repr.NewsItem]{}, merr
		}
		total = n
	}
	var next *string
	if data.NextCursor != nil && *data.NextCursor != "" {
		next = data.NextCursor
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) NewsItem(ctx context.Context, id int64) (repr.NewsItem, error) {
	if c == nil || c.News == nil {
		return repr.NewsItem{}, problem.New(problem.CodeServiceUnavailable, "", "", "news is not bound.")
	}
	rec, err := c.News.Item(ctx, id)
	if err != nil {
		if errors.Is(err, newssvc.ErrGone) {
			return repr.NewsItem{}, problem.New(problem.CodeGone, "", "", "this news item was withdrawn by its source.")
		}
		if errors.Is(err, newssvc.ErrNotFound) {
			return repr.NewsItem{}, problem.New(problem.CodeNotFound, "", "", "news item not found.")
		}
		return repr.NewsItem{}, err
	}
	return newsFromDTO(rec), nil
}

func (c *Catalog) NewsSources(ctx context.Context) (repr.List[repr.NewsSource], error) {
	if c == nil || c.News == nil {
		return repr.List[repr.NewsSource]{}, problem.New(problem.CodeServiceUnavailable, "", "", "news is not bound.")
	}
	data, err := c.News.Sources(ctx)
	if err != nil {
		return repr.List[repr.NewsSource]{}, err
	}
	items := make([]repr.NewsSource, 0, len(data.Sources))
	for _, s := range data.Sources {
		items = append(items, newsSourceFromDTO(s))
	}
	return repr.NewList(items, nil), nil
}

func (c *Catalog) mergedOrNotFound(ctx context.Context, entityType int16, object string, id int64) error {
	if c.Resolve == nil {
		return problem.New(problem.CodeNotFound, "", "", object+" not found.")
	}
	current, moved, err := c.Resolve.Resolve(ctx, entityType, id)
	if err != nil {
		return err
	}
	if !moved {
		return problem.New(problem.CodeNotFound, "", "", object+" not found.")
	}
	return problem.Merged(object, repr.ID(current), "", "",
		fmt.Sprintf("%s %d was merged into %s %d.", object, id, object, current))
}

func collectInvalidCursor() *problem.Problem {
	p := problem.New(problem.CodeInvalidCursor, "", "", "cursor could not be parsed or is no longer valid.")
	p.Errors = []problem.FieldError{{Parameter: "cursor", Reason: problem.ReasonInvalidFormat, Detail: "pass the next_cursor from a previous page of this collection"}}
	return p
}

func newsFromDTO(rec newsdto.PublicNewsItem) repr.NewsItem {
	return repr.NewsItem{
		Object: "news_item", ID: repr.ID(rec.ID), Title: rec.Title, Summary: rec.Preview,
		Source: newsSourceFromDTO(rec.Source), SourceURL: rec.SourceURL,
		Lane:        rec.Lane,
		Banner:      newsBanner(rec.BannerHash, rec.BannerURL),
		PublishedAt: rec.PublishedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// The news service already built the URL off the same cdnBase, so this reuses
// it rather than threading the base in a second time. source stays empty: it
// is the `sources` vocabulary, which names catalog anchors, and a partner's
// banner belongs to none of them.
func newsBanner(hash, url string) *repr.Image {
	if hash == "" || url == "" {
		return nil
	}
	return &repr.Image{URL: url, Hash: hash}
}

func newsSourceFromDTO(s newsdto.PublicNewsSource) repr.NewsSource {
	return repr.NewsSource{
		Object: "news_source", Name: s.Key, DisplayName: s.DisplayName,
		HomepageURL: s.HomepageURL, Attribution: s.Attribution, ColumnURL: s.ColumnURL,
	}
}

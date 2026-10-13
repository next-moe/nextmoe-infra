package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"api/internal/infrastructure/opensearch"
	"api/internal/platform/catalog/model"
	catsearch "api/internal/platform/catalog/search"
	"api/internal/testsupport/dbtest"
	"api/pkg/config"
)

func TestWorksSearchClosedVocabularies(t *testing.T) {
	for _, tok := range WorksSearchSortTokens {
		if _, ok := WorksSearchSortRule(tok); !ok {
			t.Fatalf("advertised sort token %q does not resolve", tok)
		}
	}
	if rule, _ := WorksSearchSortRule(""); rule != "" {
		t.Fatalf("empty sort must mean relevance, got %q", rule)
	}
	if rule, _ := WorksSearchSortRule("relevance"); rule != "" {
		t.Fatalf("relevance must emit no sort, got %q", rule)
	}
	for tok, want := range map[string]string{
		"released_desc": "released_ord:desc",
		"released_asc":  "released_ord:asc",
		"updated":       "updated_ts:desc",
		"popularity":    "popularity:desc",

		"rating_bangumi":      "rating.bangumi:desc",
		"rating_vndb":         "rating.vndb:desc",
		"rating_erogamescape": "rating.erogamescape:desc",
		"rating_dlsite":       "rating.dlsite:desc",
		"rating_nextmoe":      "rating.nextmoe:desc",
	} {
		if rule, _ := WorksSearchSortRule(tok); rule != want {
			t.Fatalf("sort %q = %q, want %q", tok, rule, want)
		}
	}
	if _, ok := WorksSearchSortRule("view"); ok {
		t.Fatal("sort=view must be rejected — popularity replaced it")
	}
	if _, ok := WorksSearchSortRule("nonsense"); ok {
		t.Fatal("an unknown sort token must be rejected, not ignored")
	}

	for _, tok := range WorksSearchFacetTokens {
		if !IsWorksSearchFacet(tok) {
			t.Fatalf("advertised facet token %q is not accepted", tok)
		}
	}
	for _, leaked := range []string{"tag_ids", "label_ids", "source_keys", "released_ord", "popularity"} {
		if IsWorksSearchFacet(leaked) {
			t.Fatalf("index attribute %q must not be a public facet token", leaked)
		}
	}

	got := worksSearchFacetAttrs([]string{"tag_id", "content_rating", "tag_id"})
	if len(got) != 2 || got[0] != "tag_ids" || got[1] != "content_rating" {
		t.Fatalf("facet attrs = %v, want [tag_ids content_rating]", got)
	}
}

func TestWorksSearchFacetProjection(t *testing.T) {
	dist := map[string]map[string]int64{
		"content_rating": {"0": 10, "2": 3},
		"tag_ids":        {"41": 5},
		"olang":          {"ja": 12},
	}
	out := projectWorksSearchFacets([]string{"content_rating", "tag_id", "olang"}, dist)
	if got := out["content_rating"]["all_ages"]; got != 10 {
		t.Fatalf("content_rating.all_ages = %d, want 10 (from raw key \"0\")", got)
	}
	if got := out["content_rating"]["r18"]; got != 3 {
		t.Fatalf("content_rating.r18 = %d, want 3", got)
	}
	if _, leaked := out["content_rating"]["0"]; leaked {
		t.Fatal("the raw enum key must not survive the projection")
	}
	if out["tag_id"]["41"] != 5 {
		t.Fatalf("tag_id distribution lost: %+v", out["tag_id"])
	}
	if _, leaked := out["tag_ids"]; leaked {
		t.Fatal("the index attribute name must not appear as a wire key")
	}
	if out["olang"]["ja"] != 12 {
		t.Fatalf("olang distribution lost: %+v", out["olang"])
	}
	if projectWorksSearchFacets(nil, dist) != nil {
		t.Fatal("facets must be nil when none were requested")
	}
}

func TestNormalizeVNDBID(t *testing.T) {
	for in, want := range map[string]string{
		"v19658":  "v19658",
		"V19658":  "v19658",
		" v123 ":  "v123",
		"v1":      "v1",
		"v":       "",
		"v19658a": "",
		"vndb":    "",
		"19658":   "",
		"":        "",
		"v 19658": "",
		"ヴァルキリー":  "",
	} {
		if got := normalizeVNDBID(in); got != want {
			t.Fatalf("normalizeVNDBID(%q) = %q, want %q", in, got, want)
		}
	}
}

var worksSearchTestPrefix = dbtest.SearchIndexPrefix("svc")

func sweepWorksSearchIndexes() {
	host := dbtest.SearchHost()
	if host == "" {
		return
	}
	client, err := opensearch.NewClient(config.OpenSearchConfig{
		Host: host, IndexPrefix: worksSearchTestPrefix,
	})
	if err != nil {
		return
	}
	dbtest.SweepSearchIndexes(client, worksSearchTestPrefix)
}

func worksSearchIndexer(t *testing.T) *catsearch.Indexer {
	t.Helper()
	client := dbtest.OpenSearchClient(t, worksSearchTestPrefix)
	idx := catsearch.NewOpenSearchIndexer(client)
	ctx := context.Background()
	if err := idx.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Do(context.Background(), http.MethodDelete, "/"+client.IndexName(catsearch.IndexWorks), nil, nil)
	})
	if err := idx.RecreateIndex(ctx, catsearch.IndexWorks); err != nil {
		t.Fatalf("recreate works index: %v", err)
	}
	return idx
}

func indexWorks(t *testing.T, idx *catsearch.Indexer, docs []catsearch.WorkDocInput) {
	t.Helper()
	built := make([]catsearch.EntityDoc, len(docs))
	for i, in := range docs {
		built[i] = catsearch.BuildWorkDoc(in)
	}
	if err := idx.UpsertBatch(context.Background(), catsearch.IndexWorks, built); err != nil {
		t.Fatalf("upsert works docs: %v", err)
	}
	if err := idx.Refresh(context.Background(), catsearch.IndexWorks); err != nil {
		t.Fatalf("refresh works index: %v", err)
	}
	waitIndexed(t, idx, len(docs))
}

func waitIndexed(t *testing.T, idx *catsearch.Indexer, want int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		n, err := idx.Count(t.Context(), catsearch.IndexWorks)
		if err == nil && int(n) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("index did not reach %d docs (last err %v)", want, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type searchCorpus struct {
	safe, r18, claimedWork, enWork int64
	tagID, labelID, engineID       int64
	idx                            *catsearch.Indexer
	svc                            *PublicService
}

func seedSearchCorpus(t *testing.T) searchCorpus {
	t.Helper()
	cleanTables(t)
	cleanTagTables(t)
	cleanTaxonomyTables(t)
	idx := worksSearchIndexer(t)

	c := searchCorpus{idx: idx}
	c.svc = newPublicSvc().WithWorksSearch(idx)

	safe := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "いろとりどりのセカイ")
	r18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "いろとりどりのハイパー")
	claimedW := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "クレイムド作品")
	claimWork(t, claimedW.ID, "galgame_wiki", 42)
	enWork := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Sekai Project")
	setOLang(t, enWork.ID, "en")
	c.safe, c.r18, c.claimedWork, c.enWork = safe.ID, r18.ID, claimedW.ID, enWork.ID

	c.tagID = createCanonicalTag(t, "純愛", model.TagTierCore, model.TagKindContent)
	c.labelID = addWorkLabel(t, safe.ID, "Favorite", model.LabelKindGameBrand, model.WorkLabelKindBrand)
	c.engineID = createEngine(t, "KiriKiri")
	attachEngine(t, safe.ID, c.engineID)

	createRelease(t, safe.ID, 2020, 3, 1)
	createRelease(t, r18.ID, 2010, 6, 0)
	createRelease(t, claimedW.ID, 2024, 12, 25)

	indexWorks(t, idx, []catsearch.WorkDocInput{
		{ID: safe.ID, DisplayName: "いろとりどりのセカイ", OLang: "ja",
			ContentRating: model.ContentRatingAllAges, ReleasedOrd: 20200301, UpdatedTS: 1700000001,
			Popularity: 3, TagIDs: []int64{c.tagID}, LabelIDs: []int64{c.labelID},
			EngineIDs: []int64{c.engineID}, SourceKeys: []string{"vndb"},
			Ratings: map[string]float64{"bangumi": 6.5}},
		{ID: r18.ID, DisplayName: "いろとりどりのハイパー", OLang: "ja",
			ContentRating: model.ContentRatingR18, ReleasedOrd: 20100600, UpdatedTS: 1700000002,
			Popularity: 5, TagIDs: []int64{c.tagID}, SourceKeys: []string{"vndb"}},
		{ID: claimedW.ID, DisplayName: "クレイムド作品", OLang: "ja", Claimed: true,
			ContentRating: model.ContentRatingAllAges, ReleasedOrd: 20241225, UpdatedTS: 1700000003,
			Popularity: 1, SourceKeys: []string{"bangumi"}, Ratings: map[string]float64{"bangumi": 7.9}},
		{ID: enWork.ID, DisplayName: "Sekai Project", OLang: "en",
			ContentRating: model.ContentRatingAllAges, UpdatedTS: 1700000004,
			Popularity: 9, SourceKeys: []string{"vndb"}},
	})
	return c
}

func searchIDs(t *testing.T, svc *PublicService, f WorksSearchFilter) []int64 {
	t.Helper()
	data, err := svc.WorksSearch(t.Context(), f)
	if err != nil {
		t.Fatalf("WorksSearch %+v: %v", f, err)
	}
	out := make([]int64, len(data.Items))
	for i, it := range data.Items {
		out[i] = it.ID
	}
	return out
}

func TestWorksSearchOneGate(t *testing.T) {
	c := seedSearchCorpus(t)
	ctx := t.Context()

	sfw, err := c.svc.WorksSearch(ctx, WorksSearchFilter{OLang: PublicOLang{All: true}, Facets: []string{"content_rating"}})
	if err != nil {
		t.Fatalf("sfw search: %v", err)
	}
	nsfw, err := c.svc.WorksSearch(ctx, WorksSearchFilter{NSFW: true, OLang: PublicOLang{All: true}, Facets: []string{"content_rating"}})
	if err != nil {
		t.Fatalf("nsfw search: %v", err)
	}
	if sfw.Total != 3 || nsfw.Total != 4 {
		t.Fatalf("totals sfw=%d nsfw=%d, want 3 and 4 — the r18 work must be OUT of the sfw total, not just its page",
			sfw.Total, nsfw.Total)
	}
	if _, leaked := sfw.Facets["content_rating"]["r18"]; leaked {
		t.Fatalf("sfw facet distribution leaked an r18 bucket: %+v", sfw.Facets)
	}
	if nsfw.Facets["content_rating"]["r18"] != 1 {
		t.Fatalf("nsfw facets = %+v, want one r18", nsfw.Facets)
	}

	seen := map[int64]bool{}
	for page := 1; page <= 10; page++ {
		data, err := c.svc.WorksSearch(ctx, WorksSearchFilter{
			OLang: PublicOLang{All: true}, Sort: "popularity", Page: page, Limit: 2,
		})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(data.Items) == 0 {
			break
		}
		for _, it := range data.Items {
			if seen[it.ID] {
				t.Fatalf("work %d served twice across pages", it.ID)
			}
			seen[it.ID] = true
		}
	}
	if int64(len(seen)) != sfw.Total {
		t.Fatalf("walked %d rows but total said %d — total and items disagree", len(seen), sfw.Total)
	}
	for _, id := range []int64{c.safe, c.claimedWork, c.enWork} {
		if !seen[id] {
			t.Fatalf("work %d never appeared while paging the whole set", id)
		}
	}
	if seen[c.r18] {
		t.Fatalf("r18 work %d reached an sfw caller", c.r18)
	}
}

func TestWorksSearchFiltersAreOrthogonalToText(t *testing.T) {
	c := seedSearchCorpus(t)

	if got := searchIDs(t, c.svc, WorksSearchFilter{Q: "いろとりどり", NSFW: true}); len(got) != 2 {
		t.Fatalf("text-only = %v, want both いろとりどり works", got)
	}
	if got := searchIDs(t, c.svc, WorksSearchFilter{Q: "いろとりどり", TagIDs: []int64{c.tagID}, NSFW: true}); len(got) != 2 {
		t.Fatalf("text ∧ tag = %v, want 2", got)
	}
	got := searchIDs(t, c.svc, WorksSearchFilter{Q: "いろとりどり", LabelID: c.labelID, NSFW: true})
	if len(got) != 1 || got[0] != c.safe {
		t.Fatalf("text ∧ label = %v, want [%d]", got, c.safe)
	}
	if got := searchIDs(t, c.svc, WorksSearchFilter{Q: "いろとりどり", EngineID: 999999, NSFW: true}); len(got) != 0 {
		t.Fatalf("text ∧ unknown engine = %v, want empty", got)
	}
	if got := searchIDs(t, c.svc, WorksSearchFilter{EngineID: c.engineID, NSFW: true}); len(got) != 1 || got[0] != c.safe {
		t.Fatalf("engine_id = %v, want [%d]", got, c.safe)
	}
	yes, no := true, false
	if got := searchIDs(t, c.svc, WorksSearchFilter{Claimed: &yes, OLang: PublicOLang{All: true}}); len(got) != 1 || got[0] != c.claimedWork {
		t.Fatalf("claimed=true = %v, want [%d]", got, c.claimedWork)
	}
	if got := searchIDs(t, c.svc, WorksSearchFilter{Claimed: &no, OLang: PublicOLang{All: true}}); len(got) != 2 {
		t.Fatalf("claimed=false = %v, want the two bodyless sfw works", got)
	}
	if got := searchIDs(t, c.svc, WorksSearchFilter{ReleasedAfter: 20150101, NSFW: true, OLang: PublicOLang{All: true}}); len(got) != 2 {
		t.Fatalf("released_after=2015 = %v, want the 2020 and 2024 works", got)
	}
	if got := searchIDs(t, c.svc, WorksSearchFilter{ReleasedBefore: 20151231, NSFW: true, OLang: PublicOLang{All: true}}); len(got) != 1 || got[0] != c.r18 {
		t.Fatalf("released_before=2015 = %v, want [%d]", got, c.r18)
	}
	r18 := model.ContentRatingR18
	if got := searchIDs(t, c.svc, WorksSearchFilter{ContentRating: &r18, NSFW: true, OLang: PublicOLang{All: true}}); len(got) != 1 || got[0] != c.r18 {
		t.Fatalf("content_rating=r18 = %v, want [%d]", got, c.r18)
	}
}

func TestWorksSearchOLangGate(t *testing.T) {
	c := seedSearchCorpus(t)

	got := searchIDs(t, c.svc, WorksSearchFilter{})
	for _, id := range got {
		if id == c.enWork {
			t.Fatalf("olang=en work %d survived the default ja+zh gate", c.enWork)
		}
	}
	if len(got) != 2 {
		t.Fatalf("default gate = %v, want the two sfw ja works", got)
	}
	if got := searchIDs(t, c.svc, WorksSearchFilter{OLang: PublicOLang{All: true}}); len(got) != 3 {
		t.Fatalf("olang=all = %v, want 3 sfw works", got)
	}
	if got := searchIDs(t, c.svc, WorksSearchFilter{OLang: PublicOLang{Values: []string{"en"}}}); len(got) != 1 || got[0] != c.enWork {
		t.Fatalf("olang=en = %v, want [%d]", got, c.enWork)
	}
	data, err := c.svc.WorksSearch(t.Context(), WorksSearchFilter{OLang: PublicOLang{Values: []string{"nope"}}})
	if err != nil {
		t.Fatalf("unknown olang must not error: %v", err)
	}
	if data.Total != 0 || len(data.Items) != 0 {
		t.Fatalf("unknown olang = %d items / total %d, want empty", len(data.Items), data.Total)
	}
}

func TestWorksSearchSortLanes(t *testing.T) {
	c := seedSearchCorpus(t)
	all := PublicOLang{All: true}

	desc := searchIDs(t, c.svc, WorksSearchFilter{Sort: "released_desc", OLang: all})
	if len(desc) != 3 || desc[0] != c.claimedWork || desc[1] != c.safe || desc[2] != c.enWork {
		t.Fatalf("released_desc = %v, want [2024 %d, 2020 %d, undated %d]", desc, c.claimedWork, c.safe, c.enWork)
	}
	asc := searchIDs(t, c.svc, WorksSearchFilter{Sort: "released_asc", OLang: all})
	if len(asc) != 3 || asc[0] != c.safe || asc[1] != c.claimedWork || asc[2] != c.enWork {
		t.Fatalf("released_asc = %v, want [2020 %d, 2024 %d, undated %d LAST]", asc, c.safe, c.claimedWork, c.enWork)
	}
	updated := searchIDs(t, c.svc, WorksSearchFilter{Sort: "updated", OLang: all})
	if len(updated) != 3 || updated[0] != c.enWork {
		t.Fatalf("updated = %v, want the newest-updated work %d first", updated, c.enWork)
	}
	pop := searchIDs(t, c.svc, WorksSearchFilter{Sort: "popularity", OLang: all})
	if len(pop) != 3 || pop[0] != c.enWork || pop[2] != c.claimedWork {
		t.Fatalf("popularity = %v, want [9 %d, 3 %d, 1 %d]", pop, c.enWork, c.safe, c.claimedWork)
	}
	rated := searchIDs(t, c.svc, WorksSearchFilter{Sort: "rating_bangumi", OLang: all})
	if len(rated) != 3 || rated[0] != c.claimedWork || rated[1] != c.safe || rated[2] != c.enWork {
		t.Fatalf("rating_bangumi = %v, want [7.9 %d, 6.5 %d, unrated %d LAST despite top popularity]", rated, c.claimedWork, c.safe, c.enWork)
	}
	if rel := searchIDs(t, c.svc, WorksSearchFilter{Sort: "relevance", OLang: all}); len(rel) != 3 || rel[0] != c.enWork {
		t.Fatalf("empty-q relevance = %v, want the popularity order", rel)
	}
	rel := searchIDs(t, c.svc, WorksSearchFilter{Q: "いろとりどりのセカイ", NSFW: true, OLang: all})
	if len(rel) == 0 || rel[0] != c.safe {
		t.Fatalf("text relevance = %v, want the exact title %d first", rel, c.safe)
	}
}

func TestWorksSearchVNDBShortCircuit(t *testing.T) {
	c := seedSearchCorpus(t)
	if err := testDB.Create(&model.CatalogExternalRef{
		SourceID: srcVNDB, ExternalID: "v1965", EntityType: model.EntityTypeWork,
		EntityID: c.r18, LinkKind: model.LinkKindExact,
	}).Error; err != nil {
		t.Fatalf("anchor vndb id: %v", err)
	}

	data, err := c.svc.WorksSearch(t.Context(), WorksSearchFilter{Q: "v1965", NSFW: true, OLang: PublicOLang{All: true}})
	if err != nil {
		t.Fatalf("vndb short-circuit: %v", err)
	}
	if data.Total != 1 || len(data.Items) != 1 || data.Items[0].ID != c.r18 {
		t.Fatalf("v1965 = total %d items %+v, want exactly work %d", data.Total, data.Items, c.r18)
	}
	if got := searchIDs(t, c.svc, WorksSearchFilter{Q: "V1965", NSFW: true, OLang: PublicOLang{All: true}}); len(got) != 1 || got[0] != c.r18 {
		t.Fatalf("V1965 = %v, want [%d]", got, c.r18)
	}
	if got := searchIDs(t, c.svc, WorksSearchFilter{Q: "v1965", OLang: PublicOLang{All: true}}); len(got) != 0 {
		t.Fatalf("sfw v1965 = %v, want empty (the anchored work is r18)", got)
	}
	miss, err := c.svc.WorksSearch(t.Context(), WorksSearchFilter{Q: "v999999", NSFW: true})
	if err != nil {
		t.Fatalf("unresolvable vndb id must not error: %v", err)
	}
	if miss.Total != 0 || len(miss.Items) != 0 || miss.Page != 1 {
		t.Fatalf("v999999 = %+v, want an empty page-1 envelope", miss)
	}
}

func TestWorksSearchItemsAreWorksListRows(t *testing.T) {
	c := seedSearchCorpus(t)
	addWorkTitle(t, c.safe, "zh-Hans", "五彩斑斓的世界", 0)

	data, err := c.svc.WorksSearch(t.Context(), WorksSearchFilter{
		Q: "いろとりどりのセカイ", Include: ParseWorksListInclude("names,labels"), NSFW: true,
	})
	if err != nil {
		t.Fatalf("WorksSearch include: %v", err)
	}
	if len(data.Items) == 0 {
		t.Fatal("no items")
	}
	it := data.Items[0]
	if it.ID != c.safe {
		t.Fatalf("first item = %d, want %d", it.ID, c.safe)
	}
	if it.DisplayName != "いろとりどりのセカイ" || it.ContentRating != "all_ages" || it.OLang != "ja" {
		t.Fatalf("base fields not hydrated from the registry: %+v", it)
	}
	if it.ReleaseDate == nil || *it.ReleaseDate != "2020-03-01" {
		t.Fatalf("release_date = %v, want 2020-03-01", it.ReleaseDate)
	}
	if it.Updated == "" {
		t.Fatal("updated must be present on a search row, like a browse row")
	}
	if it.Localized["zh-Hans"].Value != "五彩斑斓的世界" {
		t.Fatalf("include=names block = %+v", it.Localized)
	}
	if len(it.Labels) != 1 || it.Labels[0].ID != c.labelID {
		t.Fatalf("include=labels block = %+v", it.Labels)
	}
	plain, err := c.svc.WorksSearch(t.Context(), WorksSearchFilter{Q: "いろとりどりのセカイ", NSFW: true})
	if err != nil {
		t.Fatalf("WorksSearch plain: %v", err)
	}
	if plain.Items[0].Localized != nil || plain.Items[0].Labels != nil {
		t.Fatalf("blocks leaked without include=: %+v", plain.Items[0])
	}
	if plain.Page != 1 || plain.Limit != 20 {
		t.Fatalf("envelope page/limit = %d/%d, want 1/20", plain.Page, plain.Limit)
	}
}

func TestWorksSearchSanitizeAndThreshold(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	cleanTaxonomyTables(t)
	idx := worksSearchIndexer(t)
	svc := newPublicSvc().WithWorksSearch(idx)

	hyphen := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "CRAZY CHA!N -エルピスの鎖-")
	other := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "まったく別の作品")
	indexWorks(t, idx, []catsearch.WorkDocInput{
		{ID: hyphen.ID, DisplayName: "CRAZY CHA!N -エルピスの鎖-", OLang: "ja", UpdatedTS: 1, Popularity: 1},
		{ID: other.ID, DisplayName: "まったく別の作品", OLang: "ja", UpdatedTS: 2, Popularity: 2},
	})

	got := searchIDs(t, svc, WorksSearchFilter{Q: "CRAZY CHA!N -エルピスの鎖-"})
	if len(got) == 0 || got[0] != hyphen.ID {
		t.Fatalf("verbatim hyphenated title = %v, want [%d] — '-' was parsed as negation", got, hyphen.ID)
	}
	if got := searchIDs(t, svc, WorksSearchFilter{Q: "存在しない題名ですよこれは"}); len(got) != 0 {
		t.Fatalf("nonsense query = %v, want empty (relevance floor)", got)
	}
}

func TestWorksSearchUnavailableWithoutIndexer(t *testing.T) {
	if _, err := newPublicSvc().WorksSearch(context.Background(), WorksSearchFilter{}); err != ErrSearchUnavailable {
		t.Fatalf("err = %v, want ErrSearchUnavailable", err)
	}
}

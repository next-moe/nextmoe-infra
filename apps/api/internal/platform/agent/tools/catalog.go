package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"api/internal/platform/llm"
)

type CatalogClient struct {
	Base string
	NSFW bool
	HTTP *http.Client
}

func (c *CatalogClient) get(ctx context.Context, path string, q url.Values, out any) error {
	if c.NSFW {
		q.Set("nsfw", "true")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.Base, "/")+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("catalog %s: http %d", path, resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

type work struct {
	ID            string          `json:"id"`
	DisplayName   string          `json:"display_name"`
	ReleaseDate   *string         `json:"release_date"`
	ContentRating string          `json:"content_rating"`
	ContentLimit  string          `json:"content_limit"`
	Cover         json.RawMessage `json:"cover"`
	Localized     map[string]struct {
		Value string `json:"value"`
	} `json:"localized"`
	Intros []struct {
		Lang  string `json:"lang"`
		Value string `json:"value"`
	} `json:"intros"`
	Tags      []workTag `json:"tags"`
	Companies []struct {
		DisplayName string `json:"display_name"`
	} `json:"companies"`
	Ratings []rating `json:"ratings"`
}

type workTag struct {
	ID          *string `json:"id"`
	DisplayName string  `json:"display_name"`
	TagKind     *string `json:"tag_kind"`
	Tier        *string `json:"tier"`
	Spoiler     string  `json:"spoiler"`
	IsSexual    bool    `json:"is_sexual"`
}

// Raw site labels come first on many works ("key", "CLANNAD", staff names), so
// the first tags of a card said nothing about the work; canonical content tags
// lead, core before long tail, and raw labels only fill what is left.
func (w work) cardTags(nsfw bool, n int) []string {
	var out []string
	seen := map[string]bool{}
	add := func(t workTag) {
		key := t.DisplayName
		if t.ID != nil {
			key = "id:" + *t.ID
		}
		if len(out) == n || seen[key] || seen[t.DisplayName] || (t.IsSexual && !nsfw) || (t.Spoiler != "" && t.Spoiler != "none") {
			return
		}
		seen[key], seen[t.DisplayName] = true, true
		out = append(out, t.DisplayName)
	}
	for _, tier := range []string{"core", "longtail"} {
		for _, t := range w.Tags {
			if t.ID != nil && t.TagKind != nil && *t.TagKind == "content" && t.Tier != nil && *t.Tier == tier {
				add(t)
			}
		}
	}
	for _, t := range w.Tags {
		add(t)
	}
	return out
}

type rating struct {
	Source    string  `json:"source"`
	Score     float64 `json:"score"`
	VoteCount int     `json:"vote_count"`
}

var ratingSources = map[string]string{"bangumi": "Bangumi", "vndb": "VNDB"}

func (w work) ratings() []rating {
	var out []rating
	for _, want := range []string{"bangumi", "vndb"} {
		for _, r := range w.Ratings {
			if r.Source == want && r.Score > 0 {
				out = append(out, r)
			}
		}
	}
	return out
}

func (w work) ratingText() string {
	var parts []string
	for _, r := range w.ratings() {
		parts = append(parts, fmt.Sprintf("%s %.1f（%d 人）", ratingSources[r.Source], r.Score, r.VoteCount))
	}
	return strings.Join(parts, " · ")
}

func (w work) card(nsfw bool) Card {
	var sub []string
	facts := []Fact{}
	var companies []string
	for _, c := range w.Companies {
		if len(companies) < 2 {
			companies = append(companies, c.DisplayName)
		}
	}
	if len(companies) > 0 {
		sub = append(sub, strings.Join(companies, "、"))
		facts = append(facts, Fact{"会社", strings.Join(companies, "、")})
	}
	if w.ReleaseDate != nil && *w.ReleaseDate != "" {
		sub = append(sub, (*w.ReleaseDate)[:min(4, len(*w.ReleaseDate))])
		facts = append(facts, Fact{"发售日", *w.ReleaseDate})
	}
	tags := w.cardTags(nsfw, 6)
	if len(tags) > 0 {
		facts = append(facts, Fact{"标签", strings.Join(tags, "、")})
	}
	if r := w.ratingText(); r != "" {
		facts = append(facts, Fact{"评分", r})
	}
	var intros []intro
	for _, in := range w.Intros {
		intros = append(intros, intro{Lang: in.Lang, Value: in.Value})
	}
	ratings := []map[string]any{}
	for _, r := range w.ratings() {
		ratings = append(ratings, map[string]any{"source": r.Source, "score": r.Score, "votes": r.VoteCount})
	}
	if tags == nil {
		tags = []string{}
	}
	if companies == nil {
		companies = []string{}
	}
	raw, _ := json.Marshal(map[string]any{
		"kind": "work", "id": w.ID, "title": w.DisplayName, "localized": w.Localized,
		"release_date": w.ReleaseDate, "content_rating": w.ContentRating, "content_limit": w.ContentLimit, "cover": w.Cover,
		"subtitle": strings.Join(sub, " · "), "summary": pickIntro(intros, 160), "facts": facts,
		"tags": tags, "companies": companies, "ratings": ratings,
	})
	return Card{ID: "work-" + w.ID, Data: raw}
}

func (w work) brief() map[string]any {
	m := map[string]any{"id": w.ID, "title": w.DisplayName, "release_date": w.ReleaseDate}
	var names []string
	for lang, t := range w.Localized {
		if t.Value != "" && t.Value != w.DisplayName {
			names = append(names, lang+":"+t.Value)
		}
	}
	if len(names) > 0 {
		m["other_titles"] = names
	}
	return m
}

func (w work) searchBrief(nsfw bool) map[string]any {
	m := w.brief()
	if tags := w.cardTags(nsfw, 8); len(tags) > 0 {
		m["tags"] = tags
	}
	for _, want := range []string{"zh", "ja", ""} {
		for _, in := range w.Intros {
			if strings.HasPrefix(in.Lang, want) && in.Value != "" {
				m["intro"] = clipRunes(in.Value, 160)
				return m
			}
		}
	}
	return m
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (c *CatalogClient) searchWorks(ctx context.Context, query string, limit int) ([]work, string, error) {
	var list struct {
		Items      []work  `json:"items"`
		SearchMode *string `json:"search_mode"`
	}
	q := url.Values{"q": {query}, "search_mode": {"hybrid"}, "limit": {strconv.Itoa(limit)}, "include": {"intros,tags,companies,ratings"}}
	if err := c.get(ctx, "/v2/catalog/works", q, &list); err != nil {
		return nil, "", err
	}
	mode := "lexical"
	if list.SearchMode != nil {
		mode = *list.SearchMode
	}
	return list.Items, mode, nil
}

func (c *CatalogClient) SearchWorks(ctx context.Context, query string, limit int) ([]Card, string, error) {
	works, mode, err := c.searchWorks(ctx, query, limit)
	if err != nil {
		return nil, "", err
	}
	cards := make([]Card, len(works))
	for i, w := range works {
		cards[i] = w.card(c.NSFW)
	}
	return cards, mode, nil
}

type SearchCatalog struct{ Client *CatalogClient }

func (t *SearchCatalog) Spec() llm.Tool {
	return llm.Tool{
		Name:        "search_catalog",
		Description: "Find NextMoe catalog works (visual novels / galgames / doujin games) by title or alias in any language, or by a free-text plot description when no tag expresses it. Returns up to 8 works with ids. For themes, moods and elements prefer search_tags then browse_works.",
		Parameters: schema(map[string]any{
			"query": map[string]any{"type": "string", "description": "A title, an alias, or a description such as 'cat-eared little sister, tearjerker'."},
		}, "query"),
	}
}

func (t *SearchCatalog) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Query) == "" {
		return Output{Text: `{"error":"query is required"}`}, nil
	}
	works, _, err := t.Client.searchWorks(ctx, in.Query, 8)
	if err != nil {
		return Output{}, err
	}
	out := Output{}
	briefs := make([]map[string]any, 0, len(works))
	for _, w := range works {
		briefs = append(briefs, w.searchBrief(t.Client.NSFW))
		out.Cards = append(out.Cards, w.card(t.Client.NSFW))
	}
	raw, _ := json.Marshal(map[string]any{"works": briefs})
	out.Text = string(raw)
	return out, nil
}

type GetWork struct{ Client *CatalogClient }

func (t *GetWork) Spec() llm.Tool {
	return llm.Tool{
		Name:        "get_work",
		Description: "Read one catalog work by id: titles, release date, companies, tags and synopsis. Use the id from search_catalog.",
		Parameters:  schema(map[string]any{"id": map[string]any{"type": "string", "description": "Catalog work id"}}, "id"),
	}
}

func (t *GetWork) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	var in struct {
		ID json.Number `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil || in.ID == "" {
		return Output{Text: `{"error":"id is required"}`}, nil
	}
	var w work
	q := url.Values{"include": {"intros,tags,companies,ratings"}}
	if err := t.Client.get(ctx, "/v2/catalog/works/"+url.PathEscape(in.ID.String()), q, &w); err != nil {
		return Output{}, err
	}
	m := w.brief()
	m["content_rating"] = w.ContentRating
	m["tags"] = w.cardTags(t.Client.NSFW, 30)
	var companies []string
	for _, c := range w.Companies {
		companies = append(companies, c.DisplayName)
	}
	m["companies"] = companies
	for _, in := range w.Intros {
		m["intro_"+in.Lang] = clip(in.Value, 1500)
	}
	raw, _ := json.Marshal(m)
	return Output{Text: string(raw), Cards: []Card{w.card(t.Client.NSFW)}}, nil
}
